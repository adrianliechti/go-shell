package shell

import (
	"net/url"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

type testPermissionCore struct {
	comObject
	handler *webViewEventHandler
}

var testPermissionCoreVtbl = [64]uintptr{
	23: windows.NewCallback(func(c *testPermissionCore, h *webViewEventHandler, token *int64) uintptr {
		(*comObject)(unsafe.Pointer(h)).call(1)
		c.handler = h
		*token = 303
		return 0
	}),
	24: windows.NewCallback(func(c *testPermissionCore, token uintptr) uintptr {
		if token != 303 {
			return 0x80070057
		}
		(*comObject)(unsafe.Pointer(c.handler)).call(2)
		c.handler = nil
		return 0
	}),
}

type testPermissionArgs struct {
	comObject
	uri   string
	kind  uint32
	state uint32
}

var testPermissionArgsVtbl = [64]uintptr{
	3: windows.NewCallback(func(a *testPermissionArgs, out **uint16) uintptr {
		encoded, err := windows.UTF16PtrFromString(a.uri)
		if err != nil {
			return 0x80070057
		}
		hr, _, _ := testSHStrDup.Call(uintptr(unsafe.Pointer(encoded)), uintptr(unsafe.Pointer(out)))
		return hr
	}),
	4: windows.NewCallback(func(a *testPermissionArgs, kind *uint32) uintptr {
		*kind = a.kind
		return 0
	}),
	7: windows.NewCallback(func(a *testPermissionArgs, state uintptr) uintptr {
		a.state = uint32(state)
		return 0
	}),
}

func TestPermissionPolicy(t *testing.T) {
	base, _ := url.Parse("http://127.0.0.1:8080/?shell_token=secret")
	const geolocation = 3
	for _, test := range []struct {
		name       string
		microphone bool
		uri        string
		kind       uint32
		state      uint32
	}{
		{"microphone not enabled", false, "http://127.0.0.1:8080/", permissionKindMicrophone, permissionStateDeny},
		{"microphone for the app", true, "http://127.0.0.1:8080/chat", permissionKindMicrophone, permissionStateAllow},
		{"microphone for another origin", true, "https://example.com/", permissionKindMicrophone, permissionStateDeny},
		{"microphone for another port", true, "http://127.0.0.1:9090/", permissionKindMicrophone, permissionStateDeny},
		{"camera", true, "http://127.0.0.1:8080/", permissionKindCamera, permissionStateDeny},
		{"other permissions keep the default", true, "http://127.0.0.1:8080/", geolocation, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			core := &testPermissionCore{comObject: comObject{vtbl: &testPermissionCoreVtbl}}
			stop, err := subscribePermissions(&core.comObject, base, test.microphone)
			if err != nil {
				t.Fatal(err)
			}
			args := &testPermissionArgs{comObject: comObject{vtbl: &testPermissionArgsVtbl}, uri: test.uri, kind: test.kind}
			runtime.GC() // COM's reference must keep the Go handler alive.
			if hr := (*comObject)(unsafe.Pointer(core.handler)).call(3, uintptr(unsafe.Pointer(core)), uintptr(unsafe.Pointer(args))); hr != 0 {
				t.Fatalf("Invoke: HRESULT %#x", hr)
			}
			if args.state != test.state {
				t.Errorf("state = %d, want %d", args.state, test.state)
			}
			stop()
			if core.handler != nil {
				t.Fatal("native handler was not removed")
			}
		})
	}
}
