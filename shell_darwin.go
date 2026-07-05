package shell

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdlib.h>

void ShellRun(const char *url, const char *title, int width, int height, int minWidth, int minHeight, int debug);
*/
import "C"

import "unsafe"

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
