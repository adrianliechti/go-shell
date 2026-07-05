package shell

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>
#include <stdint.h>

void ShellRun(const char *url, const char *title, int width, int height, int minWidth, int minHeight, int debug);
void ShellPickFolder(const char *title, uintptr_t ctx);
*/
import "C"

import (
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

	C.ShellRun(url, title, C.int(opts.Width), C.int(opts.Height), C.int(opts.MinWidth), C.int(opts.MinHeight), debug)
	return nil
}

func pickFolder(title string) (string, error) {
	t := C.CString(title)
	defer C.free(unsafe.Pointer(t))

	ch := make(chan string, 1)

	C.ShellPickFolder(t, C.uintptr_t(cgo.NewHandle(ch)))

	return <-ch, nil
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
