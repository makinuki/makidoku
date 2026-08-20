package engine

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultUserAgent is applied when a plugin does not send one. Scrapers are
// routinely rejected by anti-bot layers when the request carries a
// non-browser agent string.
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"

const (
	// defaultFetchTimeout bounds a single makinuki_fetch attempt.
	defaultFetchTimeout = 30 * time.Second
	// maxResponseBytes caps the body copied into plugin memory. The instance
	// budget is 64 MB, so a larger body cannot be handed across the boundary
	// safely.
	maxResponseBytes = 16 << 20
)

// ChallengeResolver is consulted when an anti-bot challenge blocks a request.
// It returns true once fresh clearance material is available, which lets the
// host replay the original request one time.
type ChallengeResolver interface {
	// Resolve reports whether clearance newer than usedCookie is available
	// for sourceID. usedCookie is the clearance cookie that was applied to
	// the blocked attempt, empty when none was stored.
	Resolve(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool
}

// Fetcher performs plugin HTTP requests with the daemon's native network
// stack. Running outside a browser means no CORS restrictions and no header
// stripping, so requests reach sources exactly as the plugin composed them.
type Fetcher struct {
	transport http.RoundTripper
	timeout   time.Duration
	storage   Storage
	resolver  ChallengeResolver

	mu      sync.Mutex
	clients map[string]*http.Client
}

func NewFetcher(storage Storage, resolver ChallengeResolver) *Fetcher {
	return &Fetcher{
		transport: http.DefaultTransport,
		timeout:   defaultFetchTimeout,
		storage:   storage,
		resolver:  resolver,
		clients:   map[string]*http.Client{},
	}
}

// client returns the per-source HTTP client. Each source keeps its own cookie
// jar so session cookies survive across calls without leaking between
// sources.
func (f *Fetcher) client(sourceID string) *http.Client {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[sourceID]; ok {
		return c
	}
	jar, err := cookiejar.New(nil)
	c := &http.Client{Transport: f.transport, Timeout: f.timeout}
	if err == nil {
		c.Jar = jar
	}
	f.clients[sourceID] = c
	return c
}

// Do executes req on behalf of sourceID. Upstream status codes are returned
// verbatim so the plugin performs its own mapping; only a host-side failure or
// an anti-bot challenge yields an HttpError.
func (f *Fetcher) Do(ctx context.Context, sourceID string, req HttpRequest) (*HttpResponse, *HttpError) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	resp, usedCookie, herr := f.attempt(ctx, sourceID, method, req)
	if herr != nil {
		return nil, herr
	}
	if !isChallenge(resp) {
		return resp, nil
	}

	challenge := HttpError{
		Error:   CodeCloudflareBlocked,
		Status:  resp.Status,
		URL:     req.URL,
		Message: "anti-bot challenge detected",
	}

	// Only GET and HEAD are replayed transparently. A POST or PUT is
	// returned to the plugin so it decides whether re-invoking is safe.
	if method != http.MethodGet && method != http.MethodHead {
		return nil, &challenge
	}
	if f.resolver == nil || !f.resolver.Resolve(ctx, sourceID, usedCookie, challenge) {
		return nil, &challenge
	}

	replayed, _, herr := f.attempt(ctx, sourceID, method, req)
	if herr != nil {
		return nil, herr
	}
	if isChallenge(replayed) {
		challenge.Status = replayed.Status
		challenge.Message = "anti-bot challenge persisted after clearance replay"
		return nil, &challenge
	}
	return replayed, nil
}

// attempt performs one HTTP round trip and reports the clearance cookie it
// applied, so a replay can tell stale clearance from fresh.
func (f *Fetcher) attempt(ctx context.Context, sourceID, method string, req HttpRequest) (*HttpResponse, string, *HttpError) {
	target, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || !target.IsAbs() || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, "", &HttpError{
			Error:   CodeParsingError,
			URL:     req.URL,
			Message: "makinuki_fetch requires an absolute http or https URL",
		}
	}

	var body io.Reader
	if req.Body != nil {
		body = strings.NewReader(*req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, "", &HttpError{
			Error:   CodeParsingError,
			URL:     req.URL,
			Message: "could not build request: " + err.Error(),
		}
	}
	for name, value := range req.Headers {
		httpReq.Header.Set(name, value)
	}

	cookie, userAgent := f.clearance(sourceID)
	if cookie != "" {
		httpReq.Header.Set("Cookie", mergeCookie(httpReq.Header.Get("Cookie"), "cf_clearance="+cookie))
	}
	// The stored agent must match the one the clearance cookie was issued
	// to, so it wins over the plugin's own value.
	if userAgent != "" {
		httpReq.Header.Set("User-Agent", userAgent)
	} else if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", DefaultUserAgent)
	}

	httpResp, err := f.client(sourceID).Do(httpReq)
	if err != nil {
		// A dropped connection and an expired deadline share one code, so no
		// further discrimination is needed here.
		return nil, cookie, &HttpError{
			Error:   CodeNetworkTimeout,
			URL:     target.String(),
			Message: err.Error(),
		}
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, cookie, &HttpError{
			Error:   CodeNetworkTimeout,
			Status:  httpResp.StatusCode,
			URL:     target.String(),
			Message: "response body read failed: " + err.Error(),
		}
	}
	if len(raw) > maxResponseBytes {
		return nil, cookie, &HttpError{
			Error:   CodeMemoryLimitExceeded,
			Status:  httpResp.StatusCode,
			URL:     target.String(),
			Message: "response body exceeds the 16 MB host transfer cap",
		}
	}

	return &HttpResponse{
		Status:  httpResp.StatusCode,
		Headers: flattenHeaders(httpResp.Header),
		Body:    string(raw),
	}, cookie, nil
}

// clearance loads the anti-bot material recorded for sourceID.
func (f *Fetcher) clearance(sourceID string) (cookie, userAgent string) {
	if f.storage == nil {
		return "", ""
	}
	if value, ok, err := f.storage.Get(sourceID, ClearanceCookieKey); err == nil && ok {
		cookie = value
	}
	if value, ok, err := f.storage.Get(sourceID, ClearanceUserAgentKey); err == nil && ok {
		userAgent = value
	}
	return cookie, userAgent
}

// isChallenge reports whether a response is an anti-bot interstitial rather
// than source content. A 403 counts on its own; a 503 needs challenge markers
// so ordinary upstream outages stay visible to the plugin as a 503.
func isChallenge(resp *HttpResponse) bool {
	if resp == nil {
		return false
	}
	if resp.Status == http.StatusForbidden {
		return true
	}
	if resp.Status != http.StatusServiceUnavailable {
		return false
	}
	return hasChallengeMarkers(resp)
}

var challengeMarkers = []string{
	"cf-browser-verification",
	"challenge-platform",
	"cf_chl_opt",
	"__cf_chl",
	"turnstile",
	"just a moment",
	"checking your browser",
}

func hasChallengeMarkers(resp *HttpResponse) bool {
	for name := range resp.Headers {
		if strings.EqualFold(name, "cf-mitigated") {
			return true
		}
	}
	body := strings.ToLower(resp.Body)
	if len(body) > 64<<10 {
		body = body[:64<<10]
	}
	for _, marker := range challengeMarkers {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

// mergeCookie appends an entry to an existing Cookie header value.
func mergeCookie(existing, entry string) string {
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return entry
	}
	return strings.TrimSuffix(existing, ";") + "; " + entry
}

// flattenHeaders lowercases header names and joins repeated values.
func flattenHeaders(header http.Header) map[string]string {
	out := make(map[string]string, len(header))
	for name, values := range header {
		out[strings.ToLower(name)] = strings.Join(values, ", ")
	}
	return out
}
