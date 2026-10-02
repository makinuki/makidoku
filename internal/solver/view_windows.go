//go:build windows

package solver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

// rpcSFalse is the result of initializing an apartment that is already present on
// the thread. It is success for our purposes rather than a failure.
const rpcSFalse = 1

// Bounds so a wedged browser reports rather than hangs. The jar read is the one
// path here that completes through a callback and could fail to arrive at all.
const (
	cookieReadTimeout = 10 * time.Second
	viewCloseTimeout  = 5 * time.Second
	scriptProbeWait   = 15 * time.Second
)

// scriptProbe is injected by the self check to confirm that a document can run
// script and report back. It touches nothing outside the view.
const scriptProbe = `
(function () {
  try {
    chrome.webview.postMessage(JSON.stringify({ ok: true }));
  } catch (e) {}
})();
`

// initThread prepares the solver thread. The browser is a COM component and
// reports a missing apartment as a fatal error, so this runs before anything else
// touches the thread.
func initThread() error {
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		// An apartment that is already present on the thread reports this code, and
		// for our purposes that is success rather than a failure.
		if !errors.Is(err, syscall.Errno(rpcSFalse)) {
			return fmt.Errorf("initialize COM: %w", err)
		}
	}
	return nil
}

// platformSupport reports whether this build can present a solve window.
func platformSupport() error { return nil }

// runtimePresent reports whether the browser runtime is installed.
//
// Windows 10 and 11 ship it, but it can be absent where the browser has been
// disabled or on a trimmed image, and the caller needs a clear reason rather than
// a failure part way through a solve.
func runtimePresent() error {
	if _, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString(""); err != nil {
		return fmt.Errorf("%w: the browser runtime is not installed", ErrUnavailable)
	}
	return nil
}

// teardown releases the browser. The window and its pump are released when the
// solver thread returns.
func teardown() error {
	activeView = nil
	return nil
}

// openView embeds a browser in the solver window and shows or hides it.
//
// The window itself already exists and its pump is already running on this
// thread; only the browser inside it comes and goes. A dedicated profile keeps a
// clearance the machine already holds from being mistaken for one obtained here,
// and keeps one source clearance from being replayed against another.
func (s *Solver) openView(ctx context.Context, show bool) error {
	return s.onSolver(func() error {
		window := s.host()
		if window == nil {
			return fmt.Errorf("the solver window is not available")
		}
		if window.view != nil {
			return fmt.Errorf("a view is already open")
		}
		if s.profileDir != "" {
			if err := os.MkdirAll(s.profileDir, 0o700); err != nil {
				return fmt.Errorf("create solver profile: %w", err)
			}
		}

		chromium := edge.NewChromium()
		if chromium == nil {
			return fmt.Errorf("%w: could not create a browser view", ErrUnavailable)
		}
		if s.profileDir != "" {
			chromium.DataPath = s.profileDir
		}
		// Embedding blocks until the runtime is ready, dispatching the messages
		// that require on this thread.
		if !chromium.Embed(uintptr(window.hwnd)) {
			return fmt.Errorf("%w: the browser runtime is not installed", ErrUnavailable)
		}
		// The view is sized to the client area immediately. Waiting for a later
		// resize leaves it at its initial bounds, which shows as an empty window.
		chromium.Resize()
		if show {
			// The window is raised and focused so a challenge needing a click is
			// usable straight away rather than hidden behind other work.
			window.show()
			if err := chromium.Show(); err != nil {
				return fmt.Errorf("show browser: %w", err)
			}
		} else if err := chromium.Hide(); err != nil {
			return fmt.Errorf("hide browser: %w", err)
		}

		window.view = chromium
		activeView = chromium
		return nil
	})
}

// closeView releases the browser and hides the window. The window itself stays,
// because destroying it would stop the pump this view depends on.
func (s *Solver) closeView(ctx context.Context) {
	_ = s.onSolver(func() error {
		window := s.host()
		if window == nil || window.view == nil {
			return nil
		}
		window.view = nil
		activeView = nil
		window.hide()
		return nil
	})
}

// readCookies reads an origin jar.
//
// The read is issued on the solver thread and awaited here. That ordering is
// required rather than convenient: the answer arrives through a completion
// handler the runtime dispatches on the thread that made the call, so waiting on
// that thread would block the thread that owes the answer.
func (s *Solver) readCookies(ctx context.Context, origin string) (cookieSnapshot, error) {
	result := make(chan cookieSnapshot, 1)
	issued := make(chan struct{})

	go func() {
		s.onThread(func() {
			defer close(issued)
			if s.host() == nil || s.host().view == nil {
				result <- cookieSnapshot{jar: map[string]string{}}
				return
			}
			manager, err := s.host().cookieManager()
			if err != nil {
				result <- cookieSnapshot{jar: map[string]string{}}
				return
			}
			defer manager.Release()
			issueCookieRead(manager, origin, result)
		})
	}()

	select {
	case <-issued:
	case <-ctx.Done():
		return cookieSnapshot{}, ctx.Err()
	case <-time.After(cookieReadTimeout):
		return cookieSnapshot{}, fmt.Errorf("cookie read timed out")
	}

	select {
	case snapshot := <-result:
		return snapshot, nil
	case <-ctx.Done():
		return cookieSnapshot{}, ctx.Err()
	case <-time.After(cookieReadTimeout):
		return cookieSnapshot{}, fmt.Errorf("cookie read timed out")
	}
}

// writeCheckCookie stores a cookie through the manager so the self check can prove
// the read path returns what the write path stored.
func (s *Solver) writeCheckCookie(ctx context.Context) error {
	return s.onSolver(func() error {
		if s.host() == nil || s.host().view == nil {
			return fmt.Errorf("no view is open")
		}
		manager, err := s.host().cookieManager()
		if err != nil {
			return err
		}
		defer manager.Release()

		if err := manager.DeleteAllCookies(); err != nil {
			return fmt.Errorf("clear cookies: %w", err)
		}
		cookie, err := manager.CreateCookie(ClearanceName, "selfcheck", "makidoku.invalid", "/")
		if err != nil {
			return fmt.Errorf("create cookie: %w", err)
		}
		// The flag is set because the check reads it back. A real clearance cookie
		// is HttpOnly in practice, and an HttpOnly cookie is invisible to page
		// script, which is the whole reason the read is native.
		if err := cookie.PutIsHttpOnly(true); err != nil {
			return fmt.Errorf("set cookie flag: %w", err)
		}
		return manager.AddOrUpdateCookie(cookie)
	})
}

// checkScriptInjection confirms a document can run injected script and report
// back, which is how a solve learns what the page is showing.
func (s *Solver) checkScriptInjection(ctx context.Context) error {
	received := make(chan string, 4)
	if err := s.onSolver(func() error {
		activeView.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
			select {
			case received <- message:
			default:
			}
		}
		activeView.Init(scriptProbe)
		activeView.Navigate("about:blank")
		return nil
	}); err != nil {
		return err
	}

	select {
	case <-received:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("self check: the view did not run the injected script")
	case <-time.After(scriptProbeWait):
		return fmt.Errorf("self check: the view did not run the injected script")
	}
}

// beginSolve opens a visible view, installs the page reporter, and navigates to
// the origin.
//
// The window is always shown. Whether it opens on the daemon own initiative or
// because the user pressed a button is the caller decision and happens before
// this; once here, the solve path is the same either way.
func (s *Solver) beginSolve(origin string, sink *reports) error {
	if err := s.openView(context.Background(), true); err != nil {
		return err
	}
	return s.onSolver(func() error {
		window := s.host()
		if window == nil || window.view == nil {
			return fmt.Errorf("no view is open")
		}
		window.view.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
			sink.store(parsePageState(message))
		}
		// The script is added before navigating so it is present on the challenge
		// document as well as on whatever the challenge redirects to.
		window.view.Init(pageReportScript)
		window.view.Navigate(origin)
		return nil
	})
}

// host returns the solver window in its concrete type. The Solver holds it
// behind an interface so the package builds where there is no browser, and this
// is where that indirection is undone.
func (s *Solver) host() *hostWindow {
	window, _ := s.window.(*hostWindow)
	return window
}
