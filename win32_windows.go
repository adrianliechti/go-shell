//go:build windows

package shell

import (
	"syscall"
	"unsafe"
)

// Win32 surface used by the custom frame. go-webview2 has an internal w32
// package, but it is internal, so the handful of procedures and structures the
// overlay needs are declared here.

var (
	gdi32 = syscall.NewLazyDLL("gdi32.dll")

	procDefWindowProc       = user32.NewProc("DefWindowProcW")
	procCallWindowProc      = user32.NewProc("CallWindowProcW")
	procSetWindowLongPtr    = user32.NewProc("SetWindowLongPtrW")
	procCreateWindowEx      = user32.NewProc("CreateWindowExW")
	procRegisterClassEx     = user32.NewProc("RegisterClassExW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procGetClientRect       = user32.NewProc("GetClientRect")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procSetCapture          = user32.NewProc("SetCapture")
	procReleaseCapture      = user32.NewProc("ReleaseCapture")
	procTrackMouseEvent     = user32.NewProc("TrackMouseEvent")
	procInvalidateRect      = user32.NewProc("InvalidateRect")
	procBeginPaint          = user32.NewProc("BeginPaint")
	procEndPaint            = user32.NewProc("EndPaint")
	procFillRect            = user32.NewProc("FillRect")
	procDrawText            = user32.NewProc("DrawTextW")
	procPostMessage         = user32.NewProc("PostMessageW")
	procGetSystemMenu       = user32.NewProc("GetSystemMenu")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procIsZoomed            = user32.NewProc("IsZoomed")
	procIsIconic            = user32.NewProc("IsIconic")
	procIsWindow            = user32.NewProc("IsWindow")
	procGetSystemMetricsDPI = user32.NewProc("GetSystemMetricsForDpi")
	procGetDpiForWindow     = user32.NewProc("GetDpiForWindow")

	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSelectObject     = gdi32.NewProc("SelectObject")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procCreateFont       = gdi32.NewProc("CreateFontW")
)

const (
	wmSize            = 0x0005
	wmPaint           = 0x000F
	wmSettingChange   = 0x001A
	wmCaptureChanged  = 0x0215
	wmNCCalcSize      = 0x0083
	wmNCHitTest       = 0x0084
	wmNCMouseMove     = 0x00A0
	wmNCLButtonDown   = 0x00A1
	wmNCLButtonUp     = 0x00A2
	wmNCLButtonDblClk = 0x00A3
	wmSysCommand      = 0x0112
	wmMouseMove       = 0x0200
	wmLButtonDown     = 0x0201
	wmLButtonUp       = 0x0202
	wmMouseLeave      = 0x02A3
	wmNCMouseLeave    = 0x02A2
	wmDpiChanged      = 0x02E0
	wmThemeChanged    = 0x031A

	gwlpWndProc = ^uintptr(3) // -4 as an unsigned window-long index

	csHRedraw = 0x0002
	csVRedraw = 0x0001

	// Hit-test results. The button codes matter beyond routing: htMaxButton is
	// what makes Windows 11 show the Snap Layouts flyout when the pointer rests
	// over the maximize button.
	htCaption     = 2
	htSysMenu     = 3
	htMinButton   = 8
	htMaxButton   = 9
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htCloseButton = 20

	scMinimize = 0xF020
	scMaximize = 0xF030
	scRestore  = 0xF120
	scClose    = 0xF060

	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpFrameChanged = 0x0020
	swpNoActivate   = 0x0010

	hwndTop = 0

	swHide = 0
	swShow = 5

	wsChild        = 0x40000000
	wsDisabled     = 0x08000000
	wsClipSiblings = 0x04000000

	smCyCaption      = 4
	smCxMinTrack     = 34
	smCyMinTrack     = 35
	smCxSizeFrame    = 32
	smCySizeFrame    = 33
	smCxPaddedBorder = 92

	tpmReturnCmd   = 0x0100
	tpmRightButton = 0x0002

	tmeLeave     = 0x00000002
	tmeNonClient = 0x00000010

	transparentBkMode = 1

	dtCenter     = 0x00000001
	dtVCenter    = 0x00000004
	dtSingleLine = 0x00000020
	dtNoPrefix   = 0x00000800

	defaultCharset = 1
	fwNormal       = 400
)

type point struct {
	X, Y int32
}

type rect struct {
	Left, Top, Right, Bottom int32
}

type ncCalcSizeParams struct {
	Rgrc  [3]rect
	LpPos uintptr
}

type trackMouseEventStruct struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

func loWord(v uintptr) int32 {
	return int32(int16(v & 0xFFFF))
}

func hiWord(v uintptr) int32 {
	return int32(int16((v >> 16) & 0xFFFF))
}

func systemMetricForWindow(hwnd uintptr, index int) int32 {
	v, _, _ := procGetSystemMetricsDPI.Call(uintptr(index), uintptr(windowDPI(hwnd)))
	return int32(v)
}

func windowDPI(hwnd uintptr) int32 {
	if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi != 0 {
		return int32(dpi)
	}

	return 96
}

func isZoomed(hwnd uintptr) bool {
	v, _, _ := procIsZoomed.Call(hwnd)
	return v != 0
}

func isIconic(hwnd uintptr) bool {
	v, _, _ := procIsIconic.Call(hwnd)
	return v != 0
}

func isWindow(hwnd uintptr) bool {
	v, _, _ := procIsWindow.Call(hwnd)
	return v != 0
}

func clientPointToScreen(hwnd, lp uintptr) point {
	p := point{X: loWord(lp), Y: hiWord(lp)}
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return p
}

func clientRect(hwnd uintptr) rect {
	var r rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)

	if err != nil {
		return nil
	}

	return p
}
