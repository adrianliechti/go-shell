package shell

import (
	"fmt"
	"net/url"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

// Slots and values from the WebView2 SDK (WebView2.h).
const (
	coreAddPermissionRequested    = 23
	coreRemovePermissionRequested = 24
	permissionGetKind             = 4
	permissionPutState            = 7
	permissionKindMicrophone      = 1
	permissionKindCamera          = 2
	permissionStateAllow          = 1
	permissionStateDeny           = 2
)

var iidPermissionRequested = comGUID{0x15e1c6a3, 0xc72a, 0x4df3, [8]byte{0x91, 0xd7, 0xd0, 0x97, 0xfb, 0xec, 0x6b, 0xfd}}

// installPermissionPolicy replaces WebView2's media capture prompt: the
// microphone is granted to the app's origin when enabled, everything else denied.
func installPermissionPolicy(w webview2.WebView, base *url.URL, microphone bool) (func(), error) {
	core, err := webViewCore(w)
	if err != nil {
		return nil, err
	}
	stop, err := subscribePermissions(core, base, microphone)
	if err != nil {
		core.call(comRelease)
		return nil, err
	}
	return func() {
		stop()
		core.call(comRelease)
	}, nil
}

func subscribePermissions(core *comObject, base *url.URL, microphone bool) (func(), error) {
	handler := newWebViewEventHandler(iidPermissionRequested, func(args *comObject) uintptr {
		var kind uint32
		if hr := args.call(permissionGetKind, uintptr(unsafe.Pointer(&kind))); int32(hr) < 0 {
			return hr
		}
		if kind != permissionKindMicrophone && kind != permissionKindCamera {
			return 0
		}
		state := uintptr(permissionStateDeny)
		if kind == permissionKindMicrophone && microphone {
			if rawURL, hr := navigationURI(args); int32(hr) >= 0 && isAppNavigation(base, rawURL) {
				state = permissionStateAllow
			}
		}
		return args.call(permissionPutState, state)
	})
	defer handler.release()

	var token int64
	if hr := core.call(coreAddPermissionRequested, uintptr(unsafe.Pointer(handler)), uintptr(unsafe.Pointer(&token))); int32(hr) < 0 {
		return nil, fmt.Errorf("subscribe PermissionRequested: HRESULT %#x", uint32(hr))
	}
	return func() { core.call(coreRemovePermissionRequested, uintptr(token)) }, nil
}
