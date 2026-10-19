package shell

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// Slots in the stable WebView2 COM interfaces (IUnknown occupies 0..2).
const (
	controllerGetCoreWebView2    = 25
	coreAddNavigationStarting    = 7
	coreRemoveNavigationStarting = 8
	coreAddNewWindowRequested    = 44
	coreRemoveNewWindowRequested = 45
	eventGetURI                  = 3
	navigationPutCancel          = 8
	newWindowPutHandled          = 6
)

// webViewCore is the only adapter to go-webview2's private browser field. The
// pinned wrapper exposes neither its Chromium instance nor navigation events.
// Check the field and concrete type before accessing it, so an incompatible
// dependency update returns an error instead of interpreting an unknown layout.
// All subsequent access uses the exported controller and stable COM interfaces.
func webViewCore(w webview2.WebView) (*comObject, error) {
	v := reflect.ValueOf(w)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return nil, errors.New("unsupported WebView2 wrapper")
	}
	browser := v.Elem().FieldByName("browser")
	if !browser.IsValid() || browser.Kind() != reflect.Interface || browser.IsNil() {
		return nil, errors.New("WebView2 wrapper has no browser")
	}
	browser = browser.Elem()
	if browser.Type() != reflect.TypeFor[*edge.Chromium]() || browser.IsNil() {
		return nil, errors.New("unsupported WebView2 browser")
	}
	chromium := (*edge.Chromium)(browser.UnsafePointer())
	controller := chromium.GetController()
	if controller == nil {
		return nil, errors.New("WebView2 controller is not initialized")
	}
	var core *comObject
	hr := (*comObject)(unsafe.Pointer(controller)).call(controllerGetCoreWebView2, uintptr(unsafe.Pointer(&core)))
	runtime.KeepAlive(w)
	if int32(hr) < 0 {
		return nil, fmt.Errorf("get WebView2: HRESULT %#x", uint32(hr))
	}
	if core == nil {
		return nil, errors.New("WebView2 controller returned no WebView")
	}
	return core, nil // get_CoreWebView2 returns an owned reference.
}

// Native events cover ordinary links, forms, JS navigation and redirects;
// NewWindowRequested also covers window.open, named targets and middle-clicks.
// Only top-level navigation is intercepted, leaving embedded content alone.
func installNavigationPolicy(w webview2.WebView, base *url.URL) (func(), error) {
	core, err := webViewCore(w)
	if err != nil {
		return nil, err
	}
	stop, err := subscribeNavigation(core, base, w.Dispatch, w.Navigate, openExternalURL)
	if err != nil {
		core.call(comRelease)
		return nil, err
	}
	return func() {
		stop()
		core.call(comRelease)
	}, nil
}

func subscribeNavigation(core *comObject, base *url.URL, dispatch func(func()), navigate, openExternal func(string)) (func(), error) {
	navigation := newWebViewEventHandler(iidNavigationStarting, func(args *comObject) uintptr {
		rawURL, hr := navigationURI(args)
		if int32(hr) < 0 {
			args.call(navigationPutCancel, 1)
			return hr
		}
		if isAppNavigation(base, rawURL) || rawURL == "about:blank" {
			return 0
		}
		if hr := args.call(navigationPutCancel, 1); int32(hr) < 0 {
			return hr
		}
		dispatch(func() { openExternal(rawURL) })
		return 0
	})
	defer navigation.release()
	popup := newWebViewEventHandler(iidNewWindowRequested, func(args *comObject) uintptr {
		// Claim the request before reading its URI, including empty popups.
		if hr := args.call(newWindowPutHandled, 1); int32(hr) < 0 {
			return hr
		}
		rawURL, hr := navigationURI(args)
		if int32(hr) < 0 || rawURL == "" || rawURL == "about:blank" {
			return hr
		}
		// Dispatch after the event returns to avoid re-entering WebView2.
		dispatch(func() {
			if isAppNavigation(base, rawURL) {
				navigate(rawURL)
			} else {
				openExternal(rawURL)
			}
		})
		return 0
	})
	defer popup.release()

	var navigationToken, popupToken int64
	if hr := core.call(coreAddNavigationStarting, uintptr(unsafe.Pointer(navigation)), uintptr(unsafe.Pointer(&navigationToken))); int32(hr) < 0 {
		return nil, fmt.Errorf("subscribe NavigationStarting: HRESULT %#x", uint32(hr))
	}
	if hr := core.call(coreAddNewWindowRequested, uintptr(unsafe.Pointer(popup)), uintptr(unsafe.Pointer(&popupToken))); int32(hr) < 0 {
		core.call(coreRemoveNavigationStarting, uintptr(navigationToken))
		return nil, fmt.Errorf("subscribe NewWindowRequested: HRESULT %#x", uint32(hr))
	}
	return func() {
		core.call(coreRemoveNewWindowRequested, uintptr(popupToken))
		core.call(coreRemoveNavigationStarting, uintptr(navigationToken))
	}, nil
}

func navigationURI(args *comObject) (string, uintptr) {
	var raw *uint16
	hr := args.call(eventGetURI, uintptr(unsafe.Pointer(&raw)))
	if int32(hr) < 0 {
		return "", hr
	}
	if raw == nil {
		return "", 0x80004005 // E_FAIL
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(raw))
	return windows.UTF16PtrToString(raw), 0
}

var (
	iidIUnknown           = comGUID{0, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidNavigationStarting = comGUID{0x9adbe429, 0xf36d, 0x432b, [8]byte{0x9d, 0xdc, 0xf8, 0x88, 0x1f, 0xbd, 0x76, 0xe3}}
	iidNewWindowRequested = comGUID{0xd4c185fe, 0xc81c, 0x4989, [8]byte{0x97, 0xaf, 0x2d, 0x3f, 0xa7, 0xab, 0x56, 0x51}}
	webViewEventVtbl      = [4]uintptr{
		windows.NewCallback(webViewEventQueryInterface),
		windows.NewCallback(webViewEventAddRef),
		windows.NewCallback(webViewEventRelease),
		windows.NewCallback(webViewEventInvoke),
	}
	webViewEventsMu sync.Mutex
	webViewEvents   = map[*webViewEventHandler]struct{}{}
)

type webViewEventHandler struct {
	vtbl   *[4]uintptr
	iid    comGUID
	refs   atomic.Uint32
	invoke func(*comObject) uintptr
}

func newWebViewEventHandler(iid comGUID, invoke func(*comObject) uintptr) *webViewEventHandler {
	h := &webViewEventHandler{vtbl: &webViewEventVtbl, iid: iid, invoke: invoke}
	h.refs.Store(1)
	// COM references are invisible to the Go GC. Root each handler until its
	// last native/owner reference is released, including on setup failures.
	webViewEventsMu.Lock()
	webViewEvents[h] = struct{}{}
	webViewEventsMu.Unlock()
	return h
}

func (h *webViewEventHandler) release() uintptr {
	refs := h.refs.Add(^uint32(0))
	if refs == 0 {
		webViewEventsMu.Lock()
		delete(webViewEvents, h)
		webViewEventsMu.Unlock()
	}
	return uintptr(refs)
}

func webViewEventQueryInterface(h *webViewEventHandler, iid *comGUID, out *unsafe.Pointer) uintptr {
	if out == nil {
		return 0x80004003 // E_POINTER
	}
	*out = nil
	if iid == nil || (*iid != iidIUnknown && *iid != h.iid) {
		return 0x80004002 // E_NOINTERFACE
	}
	h.refs.Add(1)
	*out = unsafe.Pointer(h)
	return 0
}

func webViewEventAddRef(h *webViewEventHandler) uintptr  { return uintptr(h.refs.Add(1)) }
func webViewEventRelease(h *webViewEventHandler) uintptr { return h.release() }
func webViewEventInvoke(h *webViewEventHandler, _ *comObject, args *comObject) uintptr {
	return h.invoke(args)
}
