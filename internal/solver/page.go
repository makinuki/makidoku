package solver

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// pageState is what the injected script reports about the document in the view.
//
// The cookie alone is not enough to tell a solve from a stall. A site that is
// open serves real content and issues no cookie, and a site that is waiting for
// a click holds a challenge on screen without issuing anything. Both look
// identical if the only signal is the absence of a cookie, so the page describes
// itself as well.
type pageState struct {
	// Title and URL identify the document on screen.
	Title string `json:"title"`
	URL   string `json:"url"`
	// UserAgent is the agent the view reported, which is the identity the
	// clearance is bound to.
	UserAgent string `json:"agent"`
	// ClientHints are the header values a browser of this identity would send.
	ClientHints map[string]string `json:"hints"`
	// Challenge reports that an interstitial is on screen.
	Challenge bool `json:"challenge"`
	// Links and Text are the size of the document. A challenge page is nearly
	// empty, so these separate it from a real page when the markers do not match.
	Links int `json:"links"`
	Text  int `json:"text"`
}

// pageReportScript is added to every document the view loads.
//
// It reports the page's identity and shape, and it deliberately never touches the
// cookie store. The clearance cookie is HttpOnly and invisible to page script,
// which is the entire reason the read is native and done outside this script.
//
// The challenge markers match the ones the engine classifies on, so the solver
// and the fetcher react to the same evidence.
const pageReportScript = `
(function () {
  function hints() {
    var out = {};
    try {
      var data = navigator.userAgentData;
      if (data) {
        out['Sec-Ch-Ua'] = data.brands.map(function (b) {
          return '"' + b.brand + '";v="' + b.version + '"';
        }).join(', ');
        out['Sec-Ch-Ua-Mobile'] = data.mobile ? '?1' : '?0';
        out['Sec-Ch-Ua-Platform'] = '"' + data.platform + '"';
      }
    } catch (e) {}
    return out;
  }
  function send() {
    var marker = /just a moment|checking your browser|verify you are human|attention required|cf-challenge|challenge-platform/i;
    var text = document.body ? (document.body.innerText || '') : '';
    try {
      chrome.webview.postMessage(JSON.stringify({
        title: document.title,
        url: location.href,
        agent: navigator.userAgent,
        hints: hints(),
        challenge: marker.test(document.title + ' ' + text.slice(0, 2000)),
        links: document.getElementsByTagName('a').length,
        text: text.length
      }));
    } catch (e) {}
  }
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', send);
  } else {
    send();
  }
  // A challenge redirects and re-renders, so one report is not enough to judge
  // the page it lands on.
  setTimeout(send, 1500);
  setTimeout(send, 4000);
  setTimeout(send, 9000);
  setTimeout(send, 16000);
  setTimeout(send, 25000);
})();
`

// parsePageState reads a report message. A malformed message yields an empty
// state rather than an error, because a solve reports what it saw rather than
// validating the view's replies.
func parsePageState(message string) pageState {
	var state pageState
	if err := json.Unmarshal([]byte(message), &state); err != nil {
		return pageState{}
	}
	return state
}

// reports is the sink for page reports. It is written by the view callback and
// read by the polling loop, so access is guarded.
type reports struct {
	mu     sync.Mutex
	latest pageState
}

// newReports creates an empty sink.
func newReports() *reports { return &reports{} }

// store keeps the most recent report.
func (r *reports) store(state pageState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.latest = state
}

// current returns the most recent report.
func (r *reports) current() pageState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.latest
}

// waitForAgent returns the first report carrying a user agent, or nil when none
// arrives before the deadline.
//
// The clearance cookie is written by the network stack while the page report
// travels back as a message, so the two can arrive in either order. Returning
// the moment the cookie appears would therefore sometimes store the cookie
// without the identity it was issued to, and a clearance replayed under the
// wrong agent is the most common reason a replay fails.
func (r *reports) waitForAgent(ctx context.Context, grace time.Duration) *pageState {
	deadline := time.Now().Add(grace)
	for {
		state := r.current()
		if state.UserAgent != "" {
			return &state
		}
		if time.Now().After(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(agentPollInterval):
		}
	}
}
