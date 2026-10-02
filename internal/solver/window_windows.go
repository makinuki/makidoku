//go:build windows

package solver

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// The browser is embedded in a plain top-level window that this package owns.
// Everything here runs on the solver thread, because window messages belong to
// the thread that created the window.

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetMessage       = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procPostMessage      = user32.NewProc("PostMessageW")
	procSetForeground    = user32.NewProc("SetForegroundWindow")
	procLoadCursor       = user32.NewProc("LoadCursorW")
)

// loadStandardCursor returns the standard arrow. A missing cursor is cosmetic, so
// a failure is not reported.
func loadStandardCursor() windows.Handle {
	// MAKEINTRESOURCE(32512) is the system arrow. Passing the constant rather
	// than a string pointer is what marks it as a resource id.
	cursor, _, _ := procLoadCursor.Call(0, 32512, 0)
	return windows.Handle(cursor)
}

// Window messages this package handles itself.
const (
	// wmClose asks a window to shut down.
	wmClose = 0x0010
	// wmSize reports that the window changed size.
	wmSize = 0x0005
	// wmRunTask asks the pump to run a submitted closure. It is above the
	// system's reserved range so nothing else can collide with it.
	wmRunTask = 0x8001
)

// styleOverlapped is WS_OVERLAPPEDWINDOW.
const styleOverlapped = 0x00CF0000

// swShow is SW_SHOW and swHide is SW_HIDE.
const (
	swShow = 5
	swHide = 0
)

// hostWindow is the parent the browser is embedded in.
type hostWindow struct {
	hwnd windows.Handle
	// view is the browser embedded in the window. It is only ever touched on the
	// solver thread.
	view *edge.Chromium

	// pumped is closed once the message loop has finished, so a caller can wait
	// for the window to be gone instead of assuming it.
	pumped chan struct{}

	// task and done carry one submitted closure to the pump. They are written by
	// the submitting goroutine and read by the pump, so access is guarded.
	mu   sync.Mutex
	task func()
	done chan struct{}
}

// cookieManager returns the browser's cookie manager. It must be called on the
// solver thread, since the browser refuses the call from anywhere else.
func (w *hostWindow) cookieManager() (*edge.ICoreWebView2CookieManager, error) {
	if w.view == nil {
		return nil, fmt.Errorf("no view is open")
	}
	manager, err := w.view.GetCookieManager()
	if err != nil {
		return nil, fmt.Errorf("cookie manager: %w", err)
	}
	if manager == nil {
		return nil, fmt.Errorf("cookie manager: unavailable")
	}
	return manager, nil
}

// hostClassName is the registered window class. It is held at package scope
// because a window is created per solve while the class is registered once.
//
// Registering a class a second time fails rather than succeeding, so this is done
// exactly once per process.
var (
	hostClassName *uint16
	classOnce     sync.Once
	classErr      error
)

// registerHostClass registers the window class once per process.
func registerHostClass() (*uint16, error) {
	classOnce.Do(func() {
		name, err := windows.UTF16PtrFromString("MakidokuSolverWindow")
		if err != nil {
			classErr = err
			return
		}
		hostClassName = name

		class := wndClassEx{
			cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
			lpfnWndProc:   windows.NewCallback(hostWindowProc),
			hInstance:     windows.CurrentProcess(),
			lpszClassName: name,
			hCursor:       loadStandardCursor(),
		}
		// The error from a lazy call is the last-error value rather than a
		// reliable failure signal, so the returned atom decides.
		registered, _, _ := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
		if registered == 0 {
			classErr = fmt.Errorf("register window class failed")
		}
	})
	return hostClassName, classErr
}

// newHostWindow creates the parent window.
func newHostWindow(show bool) (*hostWindow, error) {
	className, err := registerHostClass()
	if err != nil {
		return nil, err
	}
	title, err := windows.UTF16PtrFromString("MakiDoku browser check")
	if err != nil {
		return nil, err
	}

	hwnd, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		styleOverlapped,
		0, 0, 1100, 820,
		0, 0, uintptr(windows.CurrentProcess()), 0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("create window failed")
	}
	if show {
		// The window is raised and focused so a challenge that needs a click is
		// usable straight away rather than hidden behind other work.
		procShowWindow.Call(hwnd, swShow)
		procSetForeground.Call(hwnd)
		procUpdateWindow.Call(hwnd)
	}
	return &hostWindow{hwnd: windows.Handle(hwnd), pumped: make(chan struct{})}, nil
}

// show reveals the window and brings it to the front, so a challenge that needs a
// click is usable straight away rather than hidden behind other work.
func (w *hostWindow) show() {
	procShowWindow.Call(uintptr(w.hwnd), swShow)
	procSetForeground.Call(uintptr(w.hwnd))
	procUpdateWindow.Call(uintptr(w.hwnd))
}

// hide takes the window off the screen without destroying it. The window and its
// pump have to outlive any single view, because the browser needs messages
// delivered for as long as it is alive.
func (w *hostWindow) hide() {
	procShowWindow.Call(uintptr(w.hwnd), swHide)
}

// pump dispatches window messages until the window is asked to close. The browser
// requires this: without a message loop on the owning thread, navigation and
// script execution stall.
func (w *hostWindow) pump() {
	defer close(w.pumped)
	var msg message
	for {
		got, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(got) <= 0 {
			return
		}
		switch msg.id {
		case wmClose:
			// Handled here rather than dispatched, so the loop ends without
			// waiting on a window that is on its way out.
			return
		case wmRunTask:
			w.mu.Lock()
			task, done := w.task, w.done
			w.mu.Unlock()
			if task != nil {
				task()
			}
			if done != nil {
				close(done)
			}
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// postClose wakes the pump and asks it to stop. The message goes to the window
// rather than the calling thread's queue, because the loop runs on the thread
// that owns the window.
func (w *hostWindow) postClose() {
	if w.hwnd != 0 {
		procPostMessage.Call(uintptr(w.hwnd), wmClose, 0, 0)
	}
}

// runTask runs a closure on the pump.
//
// This is how work reaches the browser. The closure is run inside the loop
// rather than on the submitting goroutine, so it must not wait for anything the
// loop itself has to deliver.
func (w *hostWindow) runTask(task func()) {
	w.mu.Lock()
	w.task = task
	w.done = make(chan struct{})
	done := w.done
	w.mu.Unlock()

	if w.hwnd == 0 {
		return
	}
	procPostMessage.Call(uintptr(w.hwnd), wmRunTask, 0, 0)
	<-done
}

func (w *hostWindow) destroy() {
	if w.hwnd != 0 {
		procDestroyWindow.Call(uintptr(w.hwnd))
		w.hwnd = 0
	}
}

// hostWindowProc forwards a message to the default handler. A window procedure
// handed to a class must be a Go function of exactly this shape; a lazy procedure
// pointer is rejected by the callback machinery.
//
// A resize is handled here because the embedded browser is a child window and
// does not grow when its parent does. Without this, maximizing the window leaves
// the view at its old bounds and bare window background shows around it.
func hostWindowProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	if msg == wmSize && activeView != nil {
		activeView.Resize()
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, msg, wparam, lparam)
	return ret
}

// activeView is the view attached to the solver thread, which is the thread the
// window procedure runs on. There is one view at a time, so a single value is
// enough.
var activeView *edge.Chromium

// wndClassEx mirrors WNDCLASSEXW.
type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

// message mirrors MSG. The trailing field is part of the structure on 64-bit
// Windows, and leaving it out makes the structure too small for the call that
// fills it, which corrupts the memory beside it.
type message struct {
	hwnd     windows.Handle
	id       uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

type point struct{ x, y int32 }

// startHost creates the window the browser will be driven from. It is hidden
// until a solve needs it on screen.
func startHost() (threadHost, error) {
	window, err := newHostWindow(false)
	if err != nil {
		return nil, err
	}
	return window, nil
}

// run pumps messages for the life of the solver and releases the window on the
// way out.
func (w *hostWindow) run() {
	w.pump()
	w.destroy()
}
