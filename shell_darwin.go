package shell

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
#include <stdint.h>

void ShellRun(const char *url, const char *title, int width, int height, int minWidth, int minHeight, int debug, int microphone, int titleBarOverlay, int titleBarHeight, int controlsOffsetX, int controlsOffsetY, const char *shellEnvironment, const char *fileMenu, uintptr_t shutdownContext);
void ShellPickFolder(const char *title, uintptr_t ctx);
void ShellScheduleNewWindows(void);
int ShellOpenWindow(const char *url);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"runtime/cgo"
	"sync"
	"unsafe"
)

type windowRequest struct {
	address string
	result  chan error
}

// AppKit processes pending requests on the main thread. Shutdown takes any
// that are still queued so callers can finish even after the event loop stops.
var windowQueue struct {
	sync.Mutex
	startURL string
	pending  []windowRequest
}

func run(opts Options) error {
	url := C.CString(opts.URL)
	defer C.free(unsafe.Pointer(url))

	title := C.CString(opts.Title)
	defer C.free(unsafe.Pointer(title))

	debug := C.int(0)

	if opts.Debug {
		debug = 1
	}
	microphone := C.int(0)
	if opts.Microphone {
		microphone = 1
	}
	overlay := C.int(0)
	if opts.TitleBar.Overlay {
		overlay = 1
	}

	menuJSON, err := json.Marshal(opts.FileMenu)
	if err != nil {
		return err
	}
	menu := C.CString(string(menuJSON))
	defer C.free(unsafe.Pointer(menu))

	environment := C.CString(shellEnvironmentScript("macos", opts.TitleBar.Overlay))
	defer C.free(unsafe.Pointer(environment))

	var shutdownHandle cgo.Handle
	if opts.shutdown != nil {
		shutdownHandle = cgo.NewHandle(opts.shutdown)
		defer shutdownHandle.Delete()
	}

	windowQueue.Lock()
	windowQueue.startURL = opts.URL
	windowQueue.Unlock()
	defer shellDrainWindowRequests(1)

	C.ShellRun(
		url,
		title,
		C.int(opts.Width),
		C.int(opts.Height),
		C.int(opts.MinWidth),
		C.int(opts.MinHeight),
		debug,
		microphone,
		overlay,
		C.int(opts.TitleBar.Height),
		C.int(opts.TitleBar.ControlsOffsetX),
		C.int(opts.TitleBar.ControlsOffsetY),
		environment,
		menu,
		C.uintptr_t(shutdownHandle),
	)
	return nil
}

func newWindow(params map[string]string) error {
	result, err := enqueueWindow(params)
	if err != nil {
		return err
	}
	return <-result
}

func enqueueWindow(params map[string]string) (<-chan error, error) {
	windowQueue.Lock()
	defer windowQueue.Unlock()
	if windowQueue.startURL == "" {
		return nil, errWindowNotRunning
	}

	address, err := windowURL(windowQueue.startURL, params)
	if err != nil {
		return nil, err
	}

	result := make(chan error, 1)
	windowQueue.pending = append(windowQueue.pending, windowRequest{address: address, result: result})
	if len(windowQueue.pending) == 1 {
		C.ShellScheduleNewWindows()
	}
	return result, nil
}

//export shellDrainWindowRequests
func shellDrainWindowRequests(stopping C.int) {
	windowQueue.Lock()
	if stopping != 0 {
		windowQueue.startURL = ""
	}
	requests := windowQueue.pending
	windowQueue.pending = nil
	windowQueue.Unlock()

	for _, request := range requests {
		if stopping != 0 {
			request.result <- errWindowNotRunning
			continue
		}
		request.result <- openWindow(request.address)
	}
}

func openWindow(address string) error {
	u := C.CString(address)
	defer C.free(unsafe.Pointer(u))
	if C.ShellOpenWindow(u) == 0 {
		return errors.New("shell: could not open window")
	}
	return nil
}

func pickFolder(title string) (string, error) {
	t := C.CString(title)
	defer C.free(unsafe.Pointer(t))

	ch := make(chan string, 1)

	C.ShellPickFolder(t, C.uintptr_t(cgo.NewHandle(ch)))

	return <-ch, nil
}

//export shellShutdown
func shellShutdown(ctx C.uintptr_t) {
	if ctx == 0 {
		return
	}
	cgo.Handle(ctx).Value().(func())()
}

//export shellFolderPicked
func shellFolderPicked(path *C.char, ctx C.uintptr_t) {
	handle := cgo.Handle(ctx)
	ch := handle.Value().(chan string)
	handle.Delete()

	if path == nil {
		ch <- ""
		return
	}

	ch <- C.GoString(path)
	C.free(unsafe.Pointer(path))
}
