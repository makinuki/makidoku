package engine

import (
	"compress/gzip"
	"compress/zlib"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/makinuki/makidoku/internal/settings"

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
// The values are those a Chromium request carries on Windows. Brotli and zstd
// are absent because this host cannot decode them, and advertising an encoding
// that is not decoded yields an unparsable body.
const (
	defaultAccept         = "text/html,application/xhtml+xml,application/xml;q=0.9,image/jxl,image/avif,image/webp,image/apng,*/*;q=0.8"
	defaultAcceptLang     = "en-US,en;q=0.9"
	defaultAcceptEncoding = "gzip, deflate"
	defaultCacheControl   = "max-age=0"
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
	// userAgent is presented when a request carries no clearance and no agent of
	// its own, so the transport never identifies itself as Go.
	userAgent string

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
		userAgent:    settings.DefaultUserAgent,
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
	//
	// The warm-up settles the session; it does not answer the request. The
	// original request is made again rather than handing back the warm-up's
	// document, because the two are different resources and a caller expecting a
	// chapter listing or an image cannot use the origin root in its place.
	if warmed := f.warmUp(ctx, sourceID, req.URL, pick); warmed != nil {
		if Classify(warmed.Status, warmed.Headers, warmed.Body) != ClassSolvable {
			if retried, _, retryErr := f.attemptWith(ctx, sourceID, method, req, pick); retryErr == nil {
				if Classify(retried.Status, retried.Headers, retried.Body) != ClassSolvable {
					return retried, nil
				}
			}
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
		// Material that was just applied and then refused again is marked, so the
		// record does not keep reporting clearance as usable while the site is
		// turning it away. The generation bound means a burst of parallel replays
		// counts once.
		if f.resolver != nil {
			_ = f.resolver.MarkChallenged(sourceID, originOf(req.URL))
		}
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

// warmUp makes one request to the root of the challenged host before a challenge
// is escalated to a human. Many gated origins hand out their bot-management
// cookie on a plain root visit, and the follow-up request then succeeds without
// any interaction.
//
// The warmed host is the one that answered with the challenge, not the source's
// recorded base address. A source that serves its documents from one host and its
// images from another is challenged on the image host, so warming the document
// host reaches an origin that never issued the challenge.
//
// It returns nil when the source has no recorded base address, when the warm-up
// cannot be made, or when it did not itself complete, so the caller falls through
// to the normal resolution path.
func (f *Fetcher) warmUp(ctx context.Context, sourceID, blockedURL string, pick clientPicker) *rawHTTPResponse {
	base := f.originRoot(sourceID)
	if base == "" {
		return nil
	}
	target, err := url.Parse(challengedRoot(blockedURL, base))
	if err != nil || target.Host == "" {
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

// challengedRoot returns the root address of the host that produced a challenge,
// falling back to the source's base address when the challenged address names no
// host.
func challengedRoot(blockedURL, base string) string {
	if parsed, err := url.Parse(strings.TrimSpace(blockedURL)); err == nil && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host + "/"
	}
	return base
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

// SetUserAgent changes the browser identity presented when a request carries no
// clearance and no agent of its own, so a value written in the interface takes
// effect without a restart. An empty value restores the default.
func (f *Fetcher) SetUserAgent(agent string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.TrimSpace(agent) == "" {
		f.userAgent = settings.DefaultUserAgent
		return
	}
	f.userAgent = agent
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
	agent := httpReq.Header.Get("User-Agent")
	if bundle != nil && bundle.UserAgent != "" {
		agent = bundle.UserAgent
	} else if agent == "" {
		// The transport otherwise writes a "Go-http-client/1.1" default that
		// identifies the host to the origin. The configured browser identity is
		// used instead, so an origin that refuses a Go transport on sight is not
		// refused before a challenge is ever involved.
		agent = f.userAgent
	}
	httpReq.Header.Set("User-Agent", agent)

	// The source's own base address is sent as the referrer and the origin, so an
	// asset host that refuses a request without them still serves the bytes. It
	// is resolved before the browser headers because the fetch metadata is
	// derived from the referrer.
	applyRefererHeaders(httpReq, f.baseURLs[sourceID])
	applyBrowserHeaders(httpReq, agent)

	// Client hints captured with a clearance belong to the agent that earned it,
	// so they replace the hints derived from the current agent.
	if bundle != nil {
		for name, value := range bundle.SecChUa {
			httpReq.Header.Set(name, value)
		}
	}

	started := time.Now()
	httpResp, err := pick(sourceID).Do(httpReq)
	if err != nil {
		// A dropped connection and an expired deadline share one code, so no
		// further discrimination is needed here.
		slog.Warn("source request failed",
			"source", sourceID, "method", method, "url", target.String(),
			"elapsed_ms", elapsedMs(started), "err", err)
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

	class := Classify(httpResp.StatusCode, flattenHeaders(httpResp.Header), raw)
	// A challenged or refused response is worth a record at info, because that is
	// what a report should contain without the owner raising the level. An
	// ordinary success stays at debug, because a chapter view produces a great
	// many of them.
	logFetch(ctx, sourceID, method, target, httpResp, class, started, len(raw), usedCookie)

	// A protected origin answering without a challenge is the only reliable
	// evidence that the stored material still works.
	if bundle != nil && class != ClassSolvable {
		_ = f.resolver.MarkUsable(sourceID, originOf(target.String()))
	}

	return &rawHTTPResponse{
		Status:  httpResp.StatusCode,
		Headers: flattenHeaders(httpResp.Header),
		Body:    raw,
	}, usedCookie, nil
}

// requestKind is how a request is presented to the origin. The kind decides the
// destination, mode, and priority, so a request can describe a document, an
// image, or a scripted call instead of every request claiming to be the first.
type requestKind int

const (
	// kindFetch is a scripted request: an API call, a JSON document, or a
	// wildcard Accept.
	kindFetch requestKind = iota
	// kindDocument is a top level document that was navigated to.
	kindDocument
	// kindImage is an image subresource.
	kindImage
)

// classifyRequest infers the kind from the Accept header. An absent Accept takes
// the document set, which is the request a plugin makes most often.
func classifyRequest(accept string) requestKind {
	switch {
	case accept == "", strings.Contains(accept, "text/html"):
		return kindDocument
	case strings.Contains(accept, "image/"):
		return kindImage
	default:
		return kindFetch
	}
}

// applyBrowserHeaders fills in the headers a Chromium request carries but a
// plugin often does not. Anything the plugin set explicitly is left alone, so a
// plugin that captured a real session keeps its own values.
//
// The client hints are derived from agent, so the hints and the agent cannot
// name different versions of the browser they claim to be.
func applyBrowserHeaders(req *http.Request, agent string) {
	// An absent Accept becomes the document set, which also decides how the
	// request is presented. A plugin that set its own Accept keeps it.
	accept := req.Header.Get("Accept")
	if accept == "" {
		accept = defaultAccept
		req.Header.Set("Accept", accept)
	}
	kind := classifyRequest(accept)

	fill := func(name, value string) {
		if req.Header.Get(name) == "" {
			req.Header.Set(name, value)
		}
	}
	fill("Accept-Language", defaultAcceptLang)
	fill("Accept-Encoding", defaultAcceptEncoding)
	fill("Priority", priorityFor(kind))
	// A navigation is the only request Chromium asks to upgrade and to bypass
	// the cache for; a subresource or a scripted call carries neither.
	if kind == kindDocument {
		fill("Cache-Control", defaultCacheControl)
		fill("Upgrade-Insecure-Requests", "1")
	}
	for name, value := range fetchMetadata(req, kind) {
		fill(name, value)
	}
	for name, value := range clientHints(agent) {
		fill(name, value)
	}
}

// priorityFor returns the request priority Chromium assigns to the kind. The
// document is the most urgent, a scripted call sits in the middle, and an image
// is normal.
func priorityFor(kind requestKind) string {
	switch kind {
	case kindDocument:
		return "u=0, i"
	case kindImage:
		return "u=2, i"
	default:
		return "u=1, i"
	}
}

// fetchMetadata returns the Sec-Fetch-* set for one request. The three kinds are
// the ones a plugin produces: a document navigation, an image subresource, and a
// scripted call. A navigation also carries the user activation flag, which is
// what separates it from a request the page made for itself.
func fetchMetadata(req *http.Request, kind requestKind) map[string]string {
	site := fetchSite(req)
	switch kind {
	case kindDocument:
		return map[string]string{
			"Sec-Fetch-Dest": "document",
			"Sec-Fetch-Mode": "navigate",
			"Sec-Fetch-Site": site,
			"Sec-Fetch-User": "?1",
		}
	case kindImage:
		return map[string]string{
			"Sec-Fetch-Dest": "image",
			"Sec-Fetch-Mode": "no-cors",
			"Sec-Fetch-Site": site,
		}
	default:
		mode := "cors"
		if site == "same-origin" {
			mode = "same-origin"
		}
		return map[string]string{
			"Sec-Fetch-Dest": "empty",
			"Sec-Fetch-Mode": mode,
			"Sec-Fetch-Site": site,
		}
	}
}

// fetchSite relates the request to the referrer the browser would have sent. A
// suffix match between the two hosts stands in for a registrable-domain
// comparison, so a subdomain counts as the same site.
func fetchSite(req *http.Request) string {
	referer := req.Header.Get("Referer")
	if referer == "" {
		return "none"
	}
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Hostname() == "" {
		return "none"
	}
	target := strings.ToLower(req.URL.Hostname())
	origin := strings.ToLower(parsed.Hostname())
	switch {
	case target == origin:
		return "same-origin"
	case strings.HasSuffix(target, "."+origin), strings.HasSuffix(origin, "."+target):
		return "same-site"
	default:
		return "cross-site"
	}
}

// chromiumVersion matches the major version of a Chromium based agent string.
var chromiumVersion = regexp.MustCompile(`(?:Chrome|Chromium)/(\d+)\.`)

// clientHints returns the low entropy client hints a Chromium agent attaches to
// every request. The high entropy hints are only sent after an Accept-CH
// response, so they are not part of this set. An agent that names no Chromium
// version gets none, because other brands do not send client hints.
//
// The platform and mobile flags are read back from the agent so the hints cannot
// contradict the identity they travel with. A platform the agent does not name
// leaves the platform hint off rather than guessing one.
//
// The middle brand is Chromium's randomized greasing token. Its value carries
// no meaning and must not be interpreted.
func clientHints(agent string) map[string]string {
	match := chromiumVersion.FindStringSubmatch(agent)
	if match == nil {
		return nil
	}
	version := match[1]
	hints := map[string]string{
		"Sec-Ch-Ua":        `"Chromium";v="` + version + `", "Not A(Brand";v="99", "Google Chrome";v="` + version + `"`,
		"Sec-Ch-Ua-Mobile": "?0",
	}
	platform, mobile := agentPlatform(agent)
	if platform != "" {
		hints["Sec-Ch-Ua-Platform"] = `"` + platform + `"`
	}
	if mobile {
		hints["Sec-Ch-Ua-Mobile"] = "?1"
	}
	return hints
}

// agentPlatform reads the platform and the mobile flag from a user agent. The
// returned name is the value Chromium reports in the platform hint.
func agentPlatform(agent string) (string, bool) {
	switch {
	case strings.Contains(agent, "Android"):
		return "Android", true
	case strings.Contains(agent, "CrOS"):
		return "Chrome OS", false
	case strings.Contains(agent, "Macintosh"), strings.Contains(agent, "Mac OS X"):
		return "macOS", false
	case strings.Contains(agent, "Windows"):
		return "Windows", false
	case strings.Contains(agent, "Linux"), strings.Contains(agent, "X11"):
		return "Linux", false
	default:
		return "", false
	}
}

// applyRefererHeaders sets Referer and Origin from the source's own base address.
//
// Some origins refuse an asset whose request carries no Referer.
//
// Both fall back to the request's own address when the source has no recorded
// base, and neither replaces a value a plugin set for itself.
func applyRefererHeaders(req *http.Request, base string) {
	referer := strings.TrimRight(base, "/")
	if referer == "" {
		referer = req.URL.String()
	}
	if req.Header.Get("Referer") == "" {
		req.Header.Set("Referer", referer)
	}
	if req.Header.Get("Origin") == "" {
		origin := referer
		if parsed, err := url.Parse(referer); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			origin = parsed.Scheme + "://" + parsed.Host
		}
		req.Header.Set("Origin", origin)
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

// requestIDFromContext returns the identifier assigned by the API layer, or an
// empty string when the work was not triggered by an inbound request. A
// background download has no inbound request, so the field is absent rather than
// empty in that case.
func requestIDFromContext(ctx context.Context) string {
	return middleware.GetReqID(ctx)
}

// elapsedMs renders a duration the way a record reads best: whole milliseconds,
// so a slow request and a fast refusal can be compared at a glance.
func elapsedMs(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}

// logFetch writes one record for a completed source request. The field set is
// chosen to answer the question a failed refresh raises: which source, which URL,
// what came back, how long it took, and whether stored material was applied.
//
// No body is ever written, and usedCookie is reduced to a boolean before it reaches
// the handler. A record can therefore state that clearance was applied without
// disclosing any part of it.
func logFetch(ctx context.Context, sourceID, method string, target *url.URL, resp *http.Response, class ChallengeClass, started time.Time, bodyLen int, usedCookie string) {
	attrs := []any{
		"source", sourceID,
		"method", method,
		"url", target.String(),
		"status", resp.StatusCode,
		"elapsed_ms", elapsedMs(started),
		"bytes", bodyLen,
		"classification", string(class),
	}
	// The correlation identifier is present when the request came in through the
	// API, and absent for a background download, which is why it is added
	// conditionally rather than as an empty field.
	if id := requestIDFromContext(ctx); id != "" {
		attrs = append(attrs, "request_id", id)
	}
	if usedCookie != "" {
		attrs = append(attrs, "clearance_applied", true)
	}

	// A challenge is raised to info, since that is what a report should contain
	// without the owner raising the level. An outright refusal is raised as well,
	// because it is the case this logging exists to explain. An ordinary response
	// stays at debug, because reading a chapter produces a great many of them.
	switch class {
	case ClassSolvable:
		slog.InfoContext(ctx, "source challenged", attrs...)
	case ClassTerminal, ClassRateLimited:
		slog.InfoContext(ctx, "source refused", attrs...)
	default:
		slog.DebugContext(ctx, "source request", attrs...)
	}
}
