package shell

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
#include <stdint.h>

void ShellRun(const char *url, const char *title, int width, int height, int minWidth, int minHeight, int debug, int titleBarOverlay, int controlsOffsetX, int controlsOffsetY, const char *fileMenu, uintptr_t shutdownContext);
void ShellPickFolder(const char *title, uintptr_t ctx);
*/
import "C"

import (
	"encoding/json"
	"runtime/cgo"
	"unsafe"
)

func run(opts Options) error {
	url := C.CString(opts.URL)
	defer C.free(unsafe.Pointer(url))

	title := C.CString(opts.Title)
	defer C.free(unsafe.Pointer(title))

	debug := C.int(0)

	if opts.Debug {
		debug = 1
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

	var shutdownHandle cgo.Handle
	if opts.shutdown != nil {
		shutdownHandle = cgo.NewHandle(opts.shutdown)
		defer shutdownHandle.Delete()
	}

	C.ShellRun(
		url,
		title,
		C.int(opts.Width),
		C.int(opts.Height),
		C.int(opts.MinWidth),
		C.int(opts.MinHeight),
		debug,
		overlay,
		C.int(opts.TitleBar.ControlsOffsetX),
		C.int(opts.TitleBar.ControlsOffsetY),
		menu,
		C.uintptr_t(shutdownHandle),
	)
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
