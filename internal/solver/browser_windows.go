//go:build windows

package solver

import (
	"runtime"
	"sort"
	"sync"
	"syscall"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// This file describes the shape of the browser cookie manager so the jar read
// can be issued correctly.
//
// The bundled bindings present the read as a synchronous call that returns the
// cookie list, but the interface has no such form: the third argument is a
// completion handler pointer and the list arrives by calling back into it.
// Passing a list pointer in that position makes the browser call through a table
// it never wrote, which faults the process instead of returning an error.
//
// The method order is fixed by the interface, so naming the layout here addresses
// the real slot rather than guessing at one. This is the only place that depends
// on that layout, and the self check exists to protect it: a dependency change
// that reorders the interface shows up as a failed check rather than as a crash
// during normal use.

// cookieManagerShape is the first field of the manager object.
type cookieManagerShape struct {
	table *cookieManagerTable
}

// cookieManagerTable lists the interface methods in order.
type cookieManagerTable struct {
	queryInterface                 uintptr
	addRef                         uintptr
	release                        uintptr
	createCookie                   uintptr
	copyCookie                     uintptr
	getCookies                     uintptr
	addOrUpdateCookie              uintptr
	deleteCookie                   uintptr
	deleteCookies                  uintptr
	deleteCookiesWithDomainAndPath uintptr
	deleteAllCookies               uintptr
}

// cookieSnapshot is one read of an origin jar. The values are carried to the
// caller and are never logged.
type cookieSnapshot struct {
	jar map[string]string
	// httpOnly records the flag on the clearance cookie. It is a pointer because
	// "no clearance cookie in this read" and "clearance cookie without the flag"
	// are different facts, and the self check needs to tell them apart.
	httpOnly *bool
}

// completionVtbl is the layout a completion handler presents. Every entry is a
// callback compiled from a Go function whose arguments are all pointer sized.
type completionVtbl struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr
	invoke         uintptr
}

// completionHandler receives one answer from the browser. The vtable pointer is
// the first field because the runtime locates a COM object methods through it.
type completionHandler struct {
	vtbl   *completionVtbl
	result chan cookieSnapshot
}

// liveCompletions holds every handler handed to the browser but not yet called
// back, keyed by the address the runtime was given.
//
// A handler is an ordinary Go object, so once the call that created it returns
// nothing in Go refers to it. The runtime still holds the address and calls it
// later, so the collector would otherwise reclaim it and the call would land on
// freed memory. Keeping a handle alive across the call that creates it is not
// sufficient; it has to stay reachable until the callback arrives.
var liveCompletions sync.Map

func newCompletionHandler(result chan cookieSnapshot) *completionHandler {
	handler := &completionHandler{vtbl: &completionVtblValue, result: result}
	liveCompletions.Store(uintptr(unsafe.Pointer(handler)), handler)
	return handler
}

var completionVtblValue = completionVtbl{
	queryInterface: windows.NewCallback(completionQueryInterface),
	addRef:         windows.NewCallback(completionAddRef),
	release:        windows.NewCallback(completionRelease),
	invoke:         windows.NewCallback(completionInvoke),
}

// completionInvoke receives the answer.
//
// The handler is found by key rather than by casting this pointer back to a Go
// object, because a key needs no conversion. That leaves the cookie list as the
// only place in this package where an integer becomes a pointer.
//
// The entry is removed here because the read is finished with. It has to stay in
// the registry until this point: the runtime calls back after the call that
// created the handler has already returned.
//
//go:nocheckptr
func completionInvoke(this, status, list uintptr) uintptr {
	entry, ok := liveCompletions.LoadAndDelete(this)
	if !ok {
		return 0
	}
	handler := entry.(*completionHandler)
	if status != 0 || list == 0 {
		// A failed read reports a status and no list. Reading the list regardless
		// would dereference a pointer that was never filled in.
		handler.result <- cookieSnapshot{jar: map[string]string{}}
		return 0
	}
	handler.result <- readCookieList(list)
	return 0
}

func completionQueryInterface(_, _, _ uintptr) uintptr { return 0 }
func completionAddRef(_ uintptr) uintptr               { return 1 }
func completionRelease(_ uintptr) uintptr              { return 1 }

// readCookieList reads a completed list.
//
// The runtime supplies the list as a plain integer, so recovering the object
// behind it means converting an integer back to a pointer, and there is no other
// way to reach it. The pointer check is therefore disabled for this function.
// The self check covers the call: if a dependency change alters the interface
// layout, the check fails here instead of the process faulting during normal use.
//
//go:nocheckptr
func readCookieList(list uintptr) cookieSnapshot {
	snapshot := cookieSnapshot{jar: map[string]string{}}
	items := (*edge.ICoreWebView2CookieList)(unsafe.Pointer(list))
	count, err := items.GetCount()
	if err != nil {
		return snapshot
	}
	for i := uint32(0); i < count; i++ {
		item, err := items.GetItem(i)
		if err != nil {
			continue
		}
		name, err := item.GetName()
		if err != nil {
			continue
		}
		value, _ := item.GetValue()
		snapshot.jar[name] = value
		if name == ClearanceName {
			httpOnly, _ := item.GetIsHttpOnly()
			snapshot.httpOnly = &httpOnly
		}
	}
	return snapshot
}

// issueCookieRead starts a read and returns without waiting for it. The handler
// sends the snapshot to the given channel when the browser calls back.
func issueCookieRead(manager *edge.ICoreWebView2CookieManager, origin string, result chan cookieSnapshot) {
	uri, err := windows.UTF16PtrFromString(origin)
	if err != nil {
		result <- cookieSnapshot{jar: map[string]string{}}
		return
	}
	handler := newCompletionHandler(result)
	table := (*cookieManagerShape)(unsafe.Pointer(manager)).table

	status, _, _ := syscall.Syscall(
		table.getCookies,
		3,
		uintptr(unsafe.Pointer(manager)),
		uintptr(unsafe.Pointer(uri)),
		uintptr(unsafe.Pointer(handler)),
	)
	// The handler and the string must outlive the call itself, because both
	// arguments are passed as plain integers. The registry keeps the handler
	// reachable until the callback; this only covers the call.
	runtime.KeepAlive(handler)
	runtime.KeepAlive(uri)

	if status != 0 {
		result <- cookieSnapshot{jar: map[string]string{}}
	}
}

// cookieNames lists the names in a snapshot, for diagnostics. Values are never
// included.
func cookieNames(snapshot cookieSnapshot) []string {
	names := make([]string, 0, len(snapshot.jar))
	for name := range snapshot.jar {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
