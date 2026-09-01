//go:build windows

package shell

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows paints the caption buttons and the caption background as part of the
// non-client area, so a window cannot place web content beside them the way the
// macOS traffic lights float over a full-size-content view. The overlay removes
// the caption altogether (see frame) and re-creates the pieces that have to stay
// native in a small child window stacked above the WebView2 widget: the caption
// buttons at the trailing edge.
//
// Keeping these native instead of handing them to the page preserves behaviours a
// web page cannot implement: the Windows 11 Snap Layouts flyout (which needs
// WM_NCHITTEST to answer HTMAXBUTTON), the system caption glyphs and metrics, and
// correct painting while the window is being resized.
//
// The parent frame still claims the buttons through WM_NCHITTEST so Windows 11
// can offer Snap Layouts for HTMAXBUTTON. The overlay also handles client mouse
// messages as a fallback for configurations where WebView2 receives the hit
// before it reaches the parent frame.

// button identifies one of the controls the shell draws.
type button int

const (
	buttonNone button = iota
	buttonMinimize
	buttonMaximize
	buttonClose
)

// Caption metrics in logical pixels. 46 wide is the Windows 11 caption button
// used by File Explorer and Edge, and the width the Snap Layouts flyout aligns
// itself to.
const (
	captionButtonWidth = 46
)

// overlay is one strip of native buttons inside the title bar.
type overlay struct {
	hwnd   uintptr
	parent uintptr

	buttons  []button
	trailing bool

	height int32
	dpi    int32

	dark bool

	hovered button
	pressed button
}

var (
	overlayMu    sync.Mutex
	overlays     = map[uintptr]*overlay{}
	overlayClass *uint16
	overlayErr   error
	overlayOnce  sync.Once
)

func lookupOverlay(hwnd uintptr) *overlay {
	overlayMu.Lock()
	defer overlayMu.Unlock()

	return overlays[hwnd]
}

func newOverlay(parent uintptr, buttons []button, trailing, dark bool) (*overlay, error) {
	overlayOnce.Do(registerOverlayClass)

	if overlayErr != nil {
		return nil, overlayErr
	}

	o := &overlay{
		parent:   parent,
		buttons:  buttons,
		trailing: trailing,
		dark:     dark,
		dpi:      96,
		hovered:  buttonNone,
		pressed:  buttonNone,
	}

	var hinstance windows.Handle
	windows.GetModuleHandleEx(0, nil, &hinstance)

	hwnd, _, err := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(overlayClass)),
		0,
		wsChild|wsClipSiblings,
		0, 0, 0, 0,
		parent,
		0,
		uintptr(hinstance),
		0,
	)

	if hwnd == 0 {
		return nil, err
	}

	o.hwnd = hwnd

	overlayMu.Lock()
	overlays[hwnd] = o
	overlayMu.Unlock()

	return o, nil
}

func registerOverlayClass() {
	name, err := windows.UTF16PtrFromString("shell-titlebar-overlay")

	if err != nil {
		overlayErr = err
		return
	}

	var hinstance windows.Handle
	windows.GetModuleHandleEx(0, nil, &hinstance)

	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		Style:         csHRedraw | csVRedraw,
		LpfnWndProc:   windows.NewCallback(overlayProc),
		HInstance:     uintptr(hinstance),
		LpszClassName: name,
	}

	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		overlayErr = err
		return
	}

	overlayClass = name
}

// buttonWidth is the width of a single button in physical pixels.
func (o *overlay) buttonWidth() int32 {
	return int32(captionButtonWidth) * o.dpi / 96
}

// width is the width of the whole strip in physical pixels.
func (o *overlay) width() int32 {
	return o.buttonWidth() * int32(len(o.buttons))
}

// layout pins the strip to its edge of the client area. The child window is only
// as wide as the buttons it draws, so the rest of the title bar stays web
// content: the WebView2 widget still fills the whole client area underneath, and
// the page draws its tab bar right up to the buttons.
func (o *overlay) layout(height int32) {
	o.height = height
	o.dpi = windowDPI(o.parent)

	if len(o.buttons) == 0 || height <= 0 {
		procShowWindow.Call(o.hwnd, swHide)
		return
	}

	client := clientRect(o.parent)
	width := o.width()

	x := int32(0)

	if o.trailing {
		x = client.Right - width
	}

	// HWND_TOP: the WebView2 widget was created first, so it would otherwise sit
	// above this window in the sibling Z-order and cover it.
	procSetWindowPos.Call(o.hwnd, hwndTop, uintptr(x), 0, uintptr(width), uintptr(height), swpNoActivate)
	procShowWindow.Call(o.hwnd, swShow)
	procInvalidateRect.Call(o.hwnd, 0, 1)
}

// rectOf returns a button's bounds in overlay client coordinates.
func (o *overlay) rectOf(target button) (rect, bool) {
	for i, b := range o.buttons {
		if b != target {
			continue
		}

		w := o.buttonWidth()
		return rect{Left: int32(i) * w, Top: 0, Right: int32(i+1) * w, Bottom: o.height}, true
	}

	return rect{}, false
}

// screenRectOf returns a button's bounds in screen coordinates, which is what the
// frame hit-tests against.
func (o *overlay) screenRectOf(target button) (rect, bool) {
	r, ok := o.rectOf(target)

	if !ok {
		return rect{}, false
	}

	origin := point{X: r.Left, Y: r.Top}
	procClientToScreen.Call(o.hwnd, uintptr(unsafe.Pointer(&origin)))

	return rect{
		Left:   origin.X,
		Top:    origin.Y,
		Right:  origin.X + (r.Right - r.Left),
		Bottom: origin.Y + (r.Bottom - r.Top),
	}, true
}

// buttonAt maps a screen point to one of the strip's buttons.
func (o *overlay) buttonAt(screen point) button {
	for _, b := range o.buttons {
		if r, ok := o.screenRectOf(b); ok && r.contains(screen) {
			return b
		}
	}

	return buttonNone
}

func (o *overlay) setState(hovered, pressed button) {
	if o.hovered == hovered && o.pressed == pressed {
		return
	}

	o.hovered = hovered
	o.pressed = pressed

	procInvalidateRect.Call(o.hwnd, 0, 1)
}

func (o *overlay) setDark(dark bool) {
	if o.dark == dark {
		return
	}

	o.dark = dark
	procInvalidateRect.Call(o.hwnd, 0, 1)
}

func (o *overlay) destroy() {
	overlayMu.Lock()
	delete(overlays, o.hwnd)
	overlayMu.Unlock()

	procDestroyWindow.Call(o.hwnd)
}

func overlayProc(hwnd, msg, wp, lp uintptr) uintptr {
	o := lookupOverlay(hwnd)

	if o != nil {
		f := lookupFrame(o.parent)

		switch msg {
		case wmPaint:
			o.paint()
			return 0

		case wmMouseMove:
			if f != nil {
				f.hover(cursorPosition())
				tracking := trackMouseEventStruct{
					CbSize:    uint32(unsafe.Sizeof(trackMouseEventStruct{})),
					DwFlags:   tmeLeave,
					HwndTrack: hwnd,
				}
				procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tracking)))
			}
			return 0

		case wmMouseLeave:
			if f != nil && f.pressed == buttonNone {
				f.clearHover()
			}
			return 0

		case wmLButtonDown:
			if f != nil && f.press(cursorPosition()) {
				procSetCapture.Call(hwnd)
			}
			return 0

		case wmLButtonUp:
			if f != nil && f.release(cursorPosition()) {
				procReleaseCapture.Call()
			}
			return 0

		case wmCaptureChanged:
			if f != nil && f.pressed != buttonNone {
				f.pressed = buttonNone
				f.clearHover()
			}
			return 0
		}
	}

	r, _, _ := procDefWindowProc.Call(hwnd, msg, wp, lp)
	return r
}
