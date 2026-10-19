package shell

import (
	"net/url"
	"runtime"
	"testing"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

// Exercise the real COM calls and callback ABI against an in-process fake;
// these tests need Windows, but do not create a window or launch a browser.
type testNavigationCore struct {
	comObject
	navigation, popup *webViewEventHandler
	failPopup         bool
}

var testNavigationCoreVtbl = [64]uintptr{
	7: windows.NewCallback(func(c *testNavigationCore, h *webViewEventHandler, token *int64) uintptr {
		(*comObject)(unsafe.Pointer(h)).call(1)
		c.navigation = h
		*token = 101
		return 0
	}),
	8: windows.NewCallback(func(c *testNavigationCore, token uintptr) uintptr {
		if token != 101 {
			return 0x80070057
		}
		(*comObject)(unsafe.Pointer(c.navigation)).call(2)
		c.navigation = nil
		return 0
	}),
	44: windows.NewCallback(func(c *testNavigationCore, h *webViewEventHandler, token *int64) uintptr {
		if c.failPopup {
			return 0x80004005
		}
		(*comObject)(unsafe.Pointer(h)).call(1)
		c.popup = h
		*token = 202
		return 0
	}),
	45: windows.NewCallback(func(c *testNavigationCore, token uintptr) uintptr {
		if token != 202 {
			return 0x80070057
		}
		(*comObject)(unsafe.Pointer(c.popup)).call(2)
		c.popup = nil
		return 0
	}),
}

type testNavigationArgs struct {
	comObject
	uri                string
	cancelled, handled bool
}

var testSHStrDup = windows.NewLazySystemDLL("shlwapi.dll").NewProc("SHStrDupW")

var testNavigationArgsVtbl = [64]uintptr{
	3: windows.NewCallback(func(a *testNavigationArgs, out **uint16) uintptr {
		encoded, err := windows.UTF16PtrFromString(a.uri)
		if err != nil {
			return 0x80070057
		}
		// Like WebView2's get_Uri, SHStrDupW returns a string allocated
		// with CoTaskMemAlloc, which navigationURI must release.
		hr, _, _ := testSHStrDup.Call(uintptr(unsafe.Pointer(encoded)), uintptr(unsafe.Pointer(out)))
		return hr
	}),
	6: windows.NewCallback(func(a *testNavigationArgs, handled uintptr) uintptr {
		a.handled = handled != 0
		return 0
	}),
	8: windows.NewCallback(func(a *testNavigationArgs, cancel uintptr) uintptr {
		a.cancelled = cancel != 0
		return 0
	}),
}

func TestNativeNavigationRouting(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:8080")
	for _, test := range []struct {
		name, uri          string
		popup, cancel      bool
		internal, external string
	}{
		{name: "external navigation", uri: "https://example.com/", cancel: true, external: "https://example.com/"},
		{name: "same origin navigation", uri: "http://127.0.0.1:8080/page"},
		{name: "bootstrap redirect", uri: "http://127.0.0.1:8080/"},
		{name: "blob download", uri: "blob:http://127.0.0.1:8080/id"},
		{name: "external popup", uri: "https://example.com/", popup: true, external: "https://example.com/"},
		{name: "same origin popup", uri: "http://127.0.0.1:8080/page", popup: true, internal: "http://127.0.0.1:8080/page"},
		{name: "blank popup", uri: "about:blank", popup: true},
		{name: "custom scheme", uri: "mailto:user@example.com", cancel: true, external: "mailto:user@example.com"},
	} {
		t.Run(test.name, func(t *testing.T) {
			core := &testNavigationCore{comObject: comObject{vtbl: &testNavigationCoreVtbl}}
			var queue []func()
			var internal, external string
			stop, err := subscribeNavigation(&core.comObject, base,
				func(f func()) { queue = append(queue, f) },
				func(s string) { internal = s }, func(s string) { external = s })
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(stop)
			h := core.navigation
			if test.popup {
				h = core.popup
			}
			args := &testNavigationArgs{comObject: comObject{vtbl: &testNavigationArgsVtbl}, uri: test.uri}
			runtime.GC() // COM's references must keep the Go handlers alive.
			hr := (*comObject)(unsafe.Pointer(h)).call(3, uintptr(unsafe.Pointer(core)), uintptr(unsafe.Pointer(args)))
			if hr != 0 {
				t.Fatalf("Invoke: HRESULT %#x", hr)
			}
			if args.cancelled != test.cancel || args.handled != test.popup {
				t.Fatalf("cancelled=%t, handled=%t; want %t, %t", args.cancelled, args.handled, test.cancel, test.popup)
			}
			if internal != "" || external != "" {
				t.Fatal("routing ran before callback returned")
			}
			for _, f := range queue {
				f()
			}
			if internal != test.internal || external != test.external {
				t.Errorf("routed internally to %q and externally to %q; want %q and %q", internal, external, test.internal, test.external)
			}
		})
	}
}

func TestNavigationSubscriptionCleanup(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:8080")
	for _, fail := range []bool{false, true} {
		webViewEventsMu.Lock()
		before := len(webViewEvents)
		webViewEventsMu.Unlock()
		core := &testNavigationCore{comObject: comObject{vtbl: &testNavigationCoreVtbl}, failPopup: fail}
		stop, err := subscribeNavigation(&core.comObject, base, func(func()) {}, func(string) {}, func(string) {})
		if (err != nil) != fail {
			t.Fatalf("setup error = %v, want failure %t", err, fail)
		}
		if stop != nil {
			stop()
		}
		if core.navigation != nil || core.popup != nil {
			t.Fatal("native handlers were not removed")
		}
		webViewEventsMu.Lock()
		after := len(webViewEvents)
		webViewEventsMu.Unlock()
		if after != before {
			t.Fatalf("Go handler roots leaked: before %d, after %d", before, after)
		}
	}
}

func TestWebViewCoreCompatibilityChecks(t *testing.T) {
	type wrapper struct {
		webview2.WebView
		browser any
	}
	for _, w := range []webview2.WebView{
		nil, (*wrapper)(nil), &wrapper{}, &wrapper{browser: "unexpected"},
		&wrapper{browser: (*edge.Chromium)(nil)}, &wrapper{browser: &edge.Chromium{}},
	} {
		if _, err := webViewCore(w); err == nil {
			t.Errorf("webViewCore(%T) accepted an uninitialized/unsupported wrapper", w)
		}
	}
}
