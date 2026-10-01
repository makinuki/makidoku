package engine

import (
	"compress/gzip"
	"compress/zlib"
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/makinuki/makidoku/internal/db"
)

const (
	// defaultFetchTimeout bounds a single makinuki_fetch attempt.
	defaultFetchTimeout = 30 * time.Second
	// ImageFetchTimeout bounds one daemon-owned image transfer. Covers are
	// served at the source's own resolution, so a single image can be several
	// megabytes and take far longer than a page request over a slow link.
	ImageFetchTimeout = 2 * time.Minute
	// maxResponseBytes caps the body copied into plugin memory. The instance
	// budget is 64 MB, so a larger body cannot be handed across the boundary
	// safely.
	maxResponseBytes = 16 << 20
	// challengeClaimTTL bounds how long an unresolved challenge suppresses
	// further resolution attempts for the same origin. It exists so a dismissed
	// prompt does not lock a source out for the life of the process.
	challengeClaimTTL = 5 * time.Minute
)

// Browser header defaults. Each is sent only when a plugin omits it, so a
// plugin that captured a real browser session keeps its own values.
//
// Brotli and zstd are absent because this host cannot decode them, and
// advertising an encoding that is not decoded yields an unparsable body.
const (
	defaultAccept         = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"
	defaultAcceptLang     = "en-US,en;q=0.9"
	defaultAcceptEncoding = "gzip, deflate"
)

// ChallengeResolver is consulted when an anti-bot challenge blocks a request.
// It returns true once fresh clearance material is available, which lets the
// host replay the original request one time.
type ChallengeResolver interface {
	// Resolve reports whether clearance newer than usedCookie is available for
	// the origin that produced the challenge. usedCookie is the clearance
	// cookie applied to the blocked attempt, empty when none was stored.
	Resolve(ctx context.Context, sourceID, usedCookie string, challenge HttpError) bool
	// Bundle returns the material stored for one source and origin, or nil.
	Bundle(sourceID, origin string) *db.ClearanceBundle
	// MarkUsable records that a request carrying the material succeeded.
	MarkUsable(sourceID, origin string) error
	// MarkChallenged records that material for an origin met a challenge.
	MarkChallenged(sourceID, origin string) error
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
	// imageClients use the same per-source jar as the request clients but allow
	// a longer transfer budget, because a cover or page image can be several
	// megabytes.
	imageClients map[string]*http.Client
	// jars holds one jar per source, shared by the request and image clients so a
	// session cookie earned by one request is present on the next.
	jars map[string]*cookiejar.Jar
	// baseURLs lets the warm-up request reach the origin root. A source that
	// has no recorded base URL simply skips the warm-up.
	baseURLs map[string]string
	// outstanding holds the origins whose challenge is currently being
	// resolved, mapped to the moment the claim was taken.
	outstanding map[string]time.Time
	// challenges holds the origins known to need clearance, so the host can
	// surface them and offer a solve.
	challenges map[string]*ChallengeState
}

type rawHTTPResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

func NewFetcher(storage Storage, resolver ChallengeResolver) *Fetcher {
	return &Fetcher{
		transport:    http.DefaultTransport,
		timeout:      defaultFetchTimeout,
		storage:      storage,
		resolver:     resolver,
		clients:      map[string]*http.Client{},
		imageClients: map[string]*http.Client{},
		jars:         map[string]*cookiejar.Jar{},
		baseURLs:     map[string]string{},
		outstanding:  map[string]time.Time{},
		challenges:   map[string]*ChallengeState{},
	}
}

// SetBaseURL records a source's entry point so the warm-up request can reach
// the origin root. It is called when a source is loaded.
func (f *Fetcher) SetBaseURL(sourceID, baseURL string) {
	if baseURL == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.baseURLs[sourceID] = baseURL
}

// jar returns the single cookie jar for a source. Both the plugin request
// client and the image client use it, which is what lets a session established
// by one carry into the other.
func (f *Fetcher) jar(sourceID string) *cookiejar.Jar {
	f.mu.Lock()
	defer f.mu.Unlock()
	if j, ok := f.jars[sourceID]; ok {
		return j
	}
	j, err := cookiejar.New(nil)
	if err != nil {
		// cookiejar.New only fails on an invalid public suffix list, which
		// cannot happen with the nil option. A nil jar simply means no cookie
		// persistence rather than a request failure.
		f.jars[sourceID] = nil
		return nil
	}
	f.jars[sourceID] = j
	return j
}

// client returns the per-source HTTP client used for plugin page requests.
func (f *Fetcher) client(sourceID string) *http.Client {
	// The jar is resolved before the lock is taken, because jar acquires the
	// same mutex and taking it twice would deadlock.
	jar := f.jar(sourceID)

	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[sourceID]; ok {
		return c
	}
	c := &http.Client{Transport: f.transport, Timeout: f.timeout, Jar: jar}
	f.clients[sourceID] = c
	return c
}

// imageClient returns the per-source client used for image transfers. It shares
// the request client's jar so a session cookie earned while listing chapters is
// present on the image request that follows, while allowing a longer budget.
func (f *Fetcher) imageClient(sourceID string) *http.Client {
	jar := f.jar(sourceID)

	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.imageClients[sourceID]; ok {
		return c
	}
	c := &http.Client{Transport: f.transport, Timeout: ImageFetchTimeout, Jar: jar}
	f.imageClients[sourceID] = c
	return c
}

// Do executes req on behalf of sourceID. Upstream status codes are returned
// verbatim so the plugin performs its own mapping; only a host-side failure or
// an anti-bot challenge yields an HttpError.
func (f *Fetcher) Do(ctx context.Context, sourceID string, req HttpRequest) (*HttpResponse, *HttpError) {
	resp, herr := f.do(ctx, sourceID, req)
	if herr != nil {
		return nil, herr
	}
	return &HttpResponse{
		Status:  resp.Status,
		Headers: resp.Headers,
		Body:    string(resp.Body),
	}, nil
}

// FetchImage downloads an image through the same per-source client used by
// plugin requests. Page headers, session cookies and anti-bot clearance are
// preserved, and the returned buffer is capped at the host transfer limit.
func (f *Fetcher) FetchImage(ctx context.Context, sourceID, target string, headers map[string]string) ([]byte, error) {
	resp, herr := f.doWith(ctx, sourceID, HttpRequest{
		URL:     target,
		Method:  http.MethodGet,
		Headers: headers,
	}, f.imageClient)
	if herr != nil {
		return nil, CodedError(herr.Error, "%s", herr.Message)
	}
	if resp.Status < http.StatusOK || resp.Status >= http.StatusMultipleChoices {
		return nil, CodedError(codeForStatus(resp.Status),
			"image request to %s returned HTTP %d", target, resp.Status)
	}
	return resp.Body, nil
}

func (f *Fetcher) do(ctx context.Context, sourceID string, req HttpRequest) (*rawHTTPResponse, *HttpError) {
	return f.doWith(ctx, sourceID, req, f.client)
}

// clientPicker selects the HTTP client used for one request. Page requests use
// the plugin budget; image transfers use the longer image budget.
type clientPicker func(sourceID string) *http.Client

func (f *Fetcher) doWith(ctx context.Context, sourceID string, req HttpRequest, pick clientPicker) (*rawHTTPResponse, *HttpError) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	resp, usedCookie, herr := f.attemptWith(ctx, sourceID, method, req, pick)
	if herr != nil {
		return nil, herr
	}

	class := Classify(resp.Status, resp.Headers, resp.Body)
	if class != ClassSolvable {
		// A terminal refusal keeps its status so the plugin maps it as before, and
		// is never replayed: a geographic restriction or a WAF ban is not cleared
		// by a challenge.
		return resp, nil
	}

	// A challenge is worth one cheap automated attempt before a human is
	// involved. Warming the origin up can settle the session on its own,
	// because the reply often carries the bot-management cookie the challenge
	// page was missing.
	if warmed := f.warmUp(ctx, sourceID, pick); warmed != nil {
		if Classify(warmed.Status, warmed.Headers, warmed.Body) != ClassSolvable {
			return warmed, nil
		}
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
		f.recordChallenge(sourceID, req.URL, challenge)
		return nil, &challenge
	}

	// A challenge is resolved once per origin. Callers that arrive while a
	// solve is already outstanding fail fast rather than each starting one, so
	// a burst of parallel page fetches produces one prompt and not one per
	// request.
	if !f.claimChallenge(sourceID, req.URL) {
		challenge.Message = "anti-bot challenge outstanding for this origin"
		return nil, &challenge
	}

	f.recordChallenge(sourceID, req.URL, challenge)
	// Stored material that was rejected is marked so the record reflects that it
	// no longer works. The update is bounded by the bundle's own generation, so a
	// burst of parallel failures counts once.
	if usedCookie != "" && f.resolver != nil {
		_ = f.resolver.MarkChallenged(sourceID, originOf(req.URL))
	}
	if f.resolver == nil || !f.resolver.Resolve(ctx, sourceID, usedCookie, challenge) {
		// The claim is retained: the challenge is still outstanding and the
		// visitor has not answered the prompt yet.
		return nil, &challenge
	}

	replayed, _, herr := f.attemptWith(ctx, sourceID, method, req, pick)
	if herr != nil {
		return nil, herr
	}
	if Classify(replayed.Status, replayed.Headers, replayed.Body) == ClassSolvable {
		challenge.Status = replayed.Status
		challenge.Message = "anti-bot challenge persisted after clearance replay"
		return nil, &challenge
	}
	f.releaseChallenge(sourceID, req.URL)
	f.clearChallenge(sourceID, req.URL)
	return replayed, nil
}

// challengeKey identifies a challenge by source and registrable domain. The
// origin is part of the key because clearance for one domain never applies to
// another, so two hosts on the same source are separate problems.
func challengeKey(sourceID, rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return sourceID + "|"
	}
	return sourceID + "|" + strings.ToLower(parsed.Hostname())
}

// claimChallenge reports whether this caller owns the challenge for an origin.
// The first caller wins and the rest are told to give up, which keeps a
// parallel burst from opening one solver per request.
//
// The claim persists while the challenge is outstanding rather than for the
// duration of one request, so a run of requests arriving before the visitor
// answers the prompt produces one prompt. A claim older than
// challengeClaimTTL is released so a dismissed prompt cannot lock the source
// out indefinitely.
func (f *Fetcher) claimChallenge(sourceID, rawURL string) bool {
	key := challengeKey(sourceID, rawURL)
	f.mu.Lock()
	defer f.mu.Unlock()
	if claimedAt, held := f.outstanding[key]; held {
		if time.Since(claimedAt) < challengeClaimTTL {
			return false
		}
	}
	f.outstanding[key] = time.Now()
	return true
}

// releaseChallenge frees the claim so a later request may resolve again. It is
// called once clearance has been applied, never when a solve merely failed.
func (f *Fetcher) releaseChallenge(sourceID, rawURL string) {
	key := challengeKey(sourceID, rawURL)
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.outstanding, key)
}

// recordChallenge notes that an origin needs clearance so the host can surface
// it and offer a solve.
func (f *Fetcher) recordChallenge(sourceID, rawURL string, challenge HttpError) {
	key := challengeKey(sourceID, rawURL)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.challenges == nil {
		f.challenges = map[string]*ChallengeState{}
	}
	state, ok := f.challenges[key]
	if !ok {
		state = &ChallengeState{SourceID: sourceID, Origin: key[strings.Index(key, "|")+1:]}
		f.challenges[key] = state
	}
	state.Hits++
	state.LastSeen = time.Now().Unix()
	state.Status = http.StatusForbidden
	state.URL = challenge.URL
	state.Message = challenge.Message
}

// clearChallenge removes the recorded state after a successful replay.
func (f *Fetcher) clearChallenge(sourceID, rawURL string) {
	key := challengeKey(sourceID, rawURL)
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.challenges, key)
}

// ChallengeStates returns a snapshot of the origins currently awaiting
// clearance, keyed by "sourceID|host". The returned map is a copy, so callers
// may read it while requests continue.
func (f *Fetcher) ChallengeStates() map[string]ChallengeState {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]ChallengeState, len(f.challenges))
	for key, state := range f.challenges {
		out[key] = *state
	}
	return out
}

// warmUp makes one request to the origin root before a challenge is escalated to
// a human. Many gated origins hand out their bot-management cookie on a plain
// root visit, and the follow-up request then succeeds without any interaction.
//
// It returns nil when the warm-up cannot be made or did not itself complete, so
// the caller falls through to the normal resolution path.
func (f *Fetcher) warmUp(ctx context.Context, sourceID string, pick clientPicker) *rawHTTPResponse {
	target, err := url.Parse(f.originRoot(sourceID))
	if err != nil {
		return nil
	}
	// A short budget keeps a stalled origin from delaying the real error. The
	// warm-up is an optimisation, never a blocker.
	warmCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	resp, _, herr := f.attemptWith(warmCtx, sourceID, http.MethodGet, HttpRequest{
		URL:    target.String(),
		Method: http.MethodGet,
	}, pick)
	if herr != nil {
		return nil
	}
	return resp
}

// originRoot returns the scheme and host of the source's base URL, used for the
// warm-up request.
func (f *Fetcher) originRoot(sourceID string) string {
	// A source with no recorded base URL yields an empty root and the warm-up is skipped.

	if base, ok := f.baseURLs[sourceID]; ok && base != "" {
		if parsed, err := url.Parse(base); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			return parsed.Scheme + "://" + parsed.Host + "/"
		}
	}
	return ""
}

// attempt performs one HTTP round trip and reports the clearance cookie it
// applied, so a replay can tell stale clearance from fresh.
func (f *Fetcher) attempt(ctx context.Context, sourceID, method string, req HttpRequest) (*rawHTTPResponse, string, *HttpError) {
	return f.attemptWith(ctx, sourceID, method, req, f.client)
}

func (f *Fetcher) attemptWith(ctx context.Context, sourceID, method string, req HttpRequest, pick clientPicker) (*rawHTTPResponse, string, *HttpError) {
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
	applyBrowserHeaders(httpReq)

	bundle := f.clearance(sourceID, target.String())
	usedCookie := ""
	if bundle != nil {
		if header := bundle.CookieHeader(); header != "" {
			httpReq.Header.Set("Cookie", mergeCookie(httpReq.Header.Get("Cookie"), header))
		}
		usedCookie = bundle.Cookies["cf_clearance"]
	}
	// The stored agent must match the one the clearance cookie was issued to, so
	// it wins over the plugin's value. With no stored clearance the plugin's own
	// agent is left unchanged.
	if bundle != nil && bundle.UserAgent != "" {
		httpReq.Header.Set("User-Agent", bundle.UserAgent)
	} else if httpReq.Header.Get("User-Agent") == "" {
		// The transport otherwise writes a "Go-http-client/1.1" default that
		// identifies the host to the origin. Assigning an empty slice suppresses
		// the header while leaving a plugin-supplied agent in place.
		httpReq.Header["User-Agent"] = nil
	}

	httpResp, err := pick(sourceID).Do(httpReq)
	if err != nil {
		// A dropped connection and an expired deadline share one code, so no
		// further discrimination is needed here.
		return nil, usedCookie, &HttpError{
			Error:   CodeNetworkTimeout,
			URL:     target.String(),
			Message: err.Error(),
		}
	}
	defer httpResp.Body.Close()

	// The Accept-Encoding header is set above to look like a browser, which also
	// means the transport no longer decompresses for us. Reading compressed
	// bytes and handing them to the plugin turns every marker check into a
	// search through binary noise, so the body is unpacked here.
	raw, err := io.ReadAll(io.LimitReader(decodeBody(httpResp), maxResponseBytes+1))
	if err != nil {
		return nil, usedCookie, &HttpError{
			Error:   CodeNetworkTimeout,
			Status:  httpResp.StatusCode,
			URL:     target.String(),
			Message: "response body read failed: " + err.Error(),
		}
	}
	if len(raw) > maxResponseBytes {
		return nil, usedCookie, &HttpError{
			Error:   CodeMemoryLimitExceeded,
			Status:  httpResp.StatusCode,
			URL:     target.String(),
			Message: "response body exceeds the 16 MB host transfer cap",
		}
	}

	// A protected origin answering without a challenge is the only reliable
	// evidence that the stored material still works.
	if bundle != nil && Classify(httpResp.StatusCode, nil, raw) != ClassSolvable {
		_ = f.resolver.MarkUsable(sourceID, originOf(target.String()))
	}

	return &rawHTTPResponse{
		Status:  httpResp.StatusCode,
		Headers: flattenHeaders(httpResp.Header),
		Body:    raw,
	}, usedCookie, nil
}

// applyBrowserHeaders fills in the navigation headers a plugin would normally
// send but often does not. Anything the plugin set explicitly is left alone, so
// a plugin that captured a real session keeps its own values.
func applyBrowserHeaders(req *http.Request) {
	for name, value := range map[string]string{
		"Accept":                    defaultAccept,
		"Accept-Language":           defaultAcceptLang,
		"Accept-Encoding":           defaultAcceptEncoding,
		"Cache-Control":             "no-cache",
		"Pragma":                    "no-cache",
		"Upgrade-Insecure-Requests": "1",
	} {
		if req.Header.Get(name) == "" {
			req.Header.Set(name, value)
		}
	}
}

// decodeBody wraps a response body in the decoder its Content-Encoding names.
// An encoding that is not handled here is passed through unchanged rather than
// failing the request, which keeps an unexpected encoding from turning into a
// hard failure.
func decodeBody(resp *http.Response) io.Reader {
	switch strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding"))) {
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return resp.Body
		}
		return zr
	case "deflate":
		// Some origins send raw deflate rather than the zlib wrapper, so a
		// failed zlib open falls back to the raw stream.
		zr, err := zlib.NewReader(resp.Body)
		if err != nil {
			return resp.Body
		}
		return zr
	default:
		return resp.Body
	}
}

// clearance loads the anti-bot material recorded for a source and origin. The
// whole jar is applied, not only the clearance cookie, because dropping a
// companion cookie causes an immediate re-challenge on many zones.
func (f *Fetcher) clearance(sourceID, target string) *db.ClearanceBundle {
	if f.resolver == nil {
		return nil
	}
	return f.resolver.Bundle(sourceID, originOf(target))
}

// RegistryUserAgent identifies the daemon when it fetches the source catalog.
const RegistryUserAgent = "makidoku/1 (+https://github.com/makinuki/makidoku)"

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
