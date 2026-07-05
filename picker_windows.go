package shell

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
)

type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidFileOpenDialog = comGUID{0xdc1c5a9c, 0xe88a, 0x4dde, [8]byte{0xa5, 0xa1, 0x60, 0xf8, 0x2a, 0x20, 0xae, 0xf7}}
	iidIFileOpenDialog  = comGUID{0xd57c7288, 0xd4ad, 0x4768, [8]byte{0xbe, 0x02, 0x9d, 0x96, 0x95, 0x32, 0xd9, 0x60}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1

	fosPickFolders     = 0x20
	fosForceFileSystem = 0x40

	sigdnFileSysPath = 0x80058000

	hrCancelled = 0x800704c7

	// IFileOpenDialog / IShellItem vtable slots.
	comRelease        = 2
	comShow           = 3
	comSetOptions     = 9
	comSetTitle       = 17
	comGetResult      = 20
	comGetDisplayName = 5
)

type comObject struct {
	vtbl *[32]uintptr
}

func (o *comObject) call(slot int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

// pickFolder runs the dialog's modal loop on the UI thread via Dispatch;
// the calling goroutine blocks until the user is done.
func pickFolder(title string) (string, error) {
	viewMu.Lock()
	w := view
	viewMu.Unlock()

	if w == nil {
		return "", errors.New("shell: window is not running")
	}

	type result struct {
		path string
		err  error
	}

	ch := make(chan result, 1)

	w.Dispatch(func() {
		path, err := folderDialog(uintptr(w.Window()), title)
		ch <- result{path, err}
	})

	r := <-ch
	return r.path, r.err
}

func folderDialog(owner uintptr, title string) (string, error) {
	// The UI thread is already an STA (WebView2 requires one) — an
	// "already initialized" result is fine.
	procCoInitializeEx.Call(0, coinitApartmentThreaded)

	var dialog *comObject

	if hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)),
		uintptr(unsafe.Pointer(&dialog)),
	); int32(hr) < 0 {
		return "", fmt.Errorf("shell: create folder dialog: %#x", hr)
	}

	defer dialog.call(comRelease)

	dialog.call(comSetOptions, fosPickFolders|fosForceFileSystem)

	if title != "" {
		if t, err := syscall.UTF16PtrFromString(title); err == nil {
			dialog.call(comSetTitle, uintptr(unsafe.Pointer(t)))
		}
	}

	if hr := dialog.call(comShow, owner); uint32(hr) == hrCancelled {
		return "", nil
	} else if int32(hr) < 0 {
		return "", fmt.Errorf("shell: folder dialog: %#x", hr)
	}

	var item *comObject

	if hr := dialog.call(comGetResult, uintptr(unsafe.Pointer(&item))); int32(hr) < 0 || item == nil {
		return "", nil
	}

	defer item.call(comRelease)

	var raw *uint16

	if hr := item.call(comGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&raw))); int32(hr) < 0 || raw == nil {
		return "", nil
	}

	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(raw)))

	return windows.UTF16PtrToString(raw), nil
}
