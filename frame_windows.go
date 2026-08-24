//go:build windows

package shell

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// frame removes the native caption from the WebView2 window and puts back what
// removing it takes away.
//
// go-webview2 owns the window procedure, so the frame subclasses it: our
// procedure handles the frame messages and forwards everything else to the
// original, leaving go-webview2's own resize, focus and close handling intact.
//
// Only the top edge becomes client area. The left, right and bottom borders stay
// non-client, so Windows keeps hit-testing and drawing them — the window resizes,
// snaps and animates exactly like any other, and the frame only has to answer for
// the strip along the top.
//
// The frame also owns the buttons' input. A top-level window is offered
// WM_NCHITTEST before Windows walks down to a child, and the buttons have to be
// reported as non-client anyway (HTMAXBUTTON is what triggers the Windows 11 Snap
// Layouts flyout), so their clicks arrive here as WM_NC* messages and the child
// overlay windows only paint.
type frame struct {
	hwnd     uintptr
	original uintptr

	// requested is TitleBar.Height in logical pixels; 0 means the system caption
	// height.
	requested int

	// height of the title-bar strip in physical pixels.
	height int32

	controls *overlay
	menu     *overlay

	// notify publishes the current insets to the page.
	notify func(titleBarInsets)

	// popup opens the FileMenu under the menu button.
	popup func()

	maximized bool

	// pressed is the button a non-client press went down on, so a release
	// elsewhere is discarded like any other button.
	pressed button
}

var (
	frameMu sync.Mutex
	frames  = map[uintptr]*frame{}
)

func lookupFrame(hwnd uintptr) *frame {
	frameMu.Lock()
	defer frameMu.Unlock()

	return frames[hwnd]
}

// newFrame installs the custom frame on hwnd.
func newFrame(hwnd uintptr, opts Options, dark bool, popup func(), notify func(titleBarInsets)) (*frame, error) {
	f := &frame{
		hwnd:      hwnd,
		requested: opts.TitleBar.Height,
		notify:    notify,
		popup:     popup,
		pressed:   buttonNone,
	}

	f.height = f.titleBarHeight()

	controls, err := newOverlay(hwnd, []button{buttonClose}, true, dark)

	if err != nil {
		return nil, err
	}

	f.controls = controls

	if opts.TitleBar.Menu && len(opts.FileMenu) > 0 {
		menu, err := newOverlay(hwnd, []button{buttonMenu}, false, dark)

		if err != nil {
			controls.destroy()
			return nil, err
		}

		f.menu = menu
	}

	frameMu.Lock()
	frames[hwnd] = f
	frameMu.Unlock()

	original, _, _ := procSetWindowLongPtr.Call(hwnd, gwlpWndProc, windows.NewCallback(frameProc))
	f.original = original

	// Recompute the frame now that WM_NCCALCSIZE answers differently.
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpFrameChanged)

	f.sync()

	return f, nil
}

// titleBarHeight is the height of the strip in physical pixels.
func (f *frame) titleBarHeight() int32 {
	if f.requested > 0 {
		return int32(f.requested) * windowDPI(f.hwnd) / 96
	}

	return f.captionHeight() + f.topBorder()
}

// captionHeight is the height of the caption Windows would have drawn.
func (f *frame) captionHeight() int32 {
	return systemMetric(smCyCaption)
}

// topBorder is the resize frame above the caption, which is also the grab area the
// strip has to provide for resizing from the top.
func (f *frame) topBorder() int32 {
	return systemMetric(smCySizeFrame) + systemMetric(smCxPaddedBorder)
}

// sync re-lays out the button strips and republishes the insets, after anything
// that moves them: resize, maximize, DPI change.
func (f *frame) sync() {
	if isIconic(f.hwnd) {
		return
	}

	f.maximized = isZoomed(f.hwnd)
	f.height = f.titleBarHeight()

	// layout repaints, which also picks up the maximize button's state-dependent
	// glyph — the pointer does not have to move for that to change.
	if f.controls != nil {
		f.controls.layout(f.height)
	}

	if f.menu != nil {
		f.menu.layout(f.height)
	}

	if f.notify != nil {
		f.notify(f.insets())
	}
}

// insets is the geometry the page has to keep clear, in CSS pixels.
func (f *frame) insets() titleBarInsets {
	dpi := windowDPI(f.hwnd)

	toCSS := func(v int32) int {
		return int(v * 96 / dpi)
	}

	insets := titleBarInsets{
		Height:    toCSS(f.height),
		Maximized: f.maximized,
	}

	if f.menu != nil {
		insets.Left = toCSS(f.menu.width())
	}

	if f.controls != nil {
		insets.Right = toCSS(f.controls.width())
	}

	return insets
}

func (f *frame) setDark(dark bool) {
	if f.controls != nil {
		f.controls.setDark(dark)
	}

	if f.menu != nil {
		f.menu.setDark(dark)
	}
}

// menuAnchor is the menu button's bounds in screen coordinates, which is where
// its popup is dropped.
func (f *frame) menuAnchor() rect {
	if f.menu == nil {
		return rect{}
	}

	r, _ := f.menu.screenRectOf(buttonMenu)

	return r
}

// strips are the button strips, in hit-test order.
func (f *frame) strips() []*overlay {
	strips := make([]*overlay, 0, 2)

	if f.controls != nil {
		strips = append(strips, f.controls)
	}

	if f.menu != nil {
		strips = append(strips, f.menu)
	}

	return strips
}

// buttonAt maps a screen point to a button and the strip that draws it.
func (f *frame) buttonAt(screen point) (*overlay, button) {
	for _, strip := range f.strips() {
		if b := strip.buttonAt(screen); b != buttonNone {
			return strip, b
		}
	}

	return nil, buttonNone
}

// hitTest answers WM_NCHITTEST for the strip. Everything below it, and the side
// and bottom borders, is left to the original procedure.
func (f *frame) hitTest(screen point) (uintptr, bool) {
	window := f.windowRect()

	if screen.Y >= window.Top+f.height {
		return 0, false
	}

	// The top resize edge is inside the client area now, so the strip provides it.
	// A maximized window has no resize edges.
	if !f.maximized && screen.Y < window.Top+f.topBorder() {
		border := systemMetric(smCxSizeFrame) + systemMetric(smCxPaddedBorder)

		switch {
		case screen.X < window.Left+border:
			return htTopLeft, true
		case screen.X >= window.Right-border:
			return htTopRight, true
		}

		return htTop, true
	}

	// The buttons are non-client so their clicks arrive as WM_NC* messages. The
	// codes matter beyond routing: HTMAXBUTTON is what makes Windows 11 offer the
	// Snap Layouts flyout, and HTSYSMENU gives the menu button the caption icon's
	// behaviour.
	if _, b := f.buttonAt(screen); b != buttonNone {
		f.hover(screen)

		switch b {
		case buttonMenu:
			return htSysMenu, true
		case buttonMinimize:
			return htMinButton, true
		case buttonMaximize:
			return htMaxButton, true
		case buttonClose:
			return htCloseButton, true
		}
	}

	f.clearHover()

	return 0, false
}

// hover highlights the button under the pointer. The overlay windows never see
// the pointer, so their state is driven from here.
func (f *frame) hover(screen point) {
	strip, b := f.buttonAt(screen)

	for _, other := range f.strips() {
		if other == strip {
			other.setState(b, f.pressed)
			continue
		}

		other.setState(buttonNone, buttonNone)
	}
}

func (f *frame) clearHover() {
	for _, strip := range f.strips() {
		strip.setState(buttonNone, buttonNone)
	}
}

// press records the button a non-client press went down on.
func (f *frame) press(screen point) bool {
	_, b := f.buttonAt(screen)

	if b == buttonNone {
		return false
	}

	f.pressed = b
	f.hover(screen)

	// The menu opens on press, like a menu bar, rather than on release.
	if b == buttonMenu {
		f.pressed = buttonNone

		if f.popup != nil {
			f.popup()
		}

		f.clearHover()
	}

	return true
}

// release acts on a press that ended on the same button.
func (f *frame) release(screen point) bool {
	pressed := f.pressed

	if pressed == buttonNone {
		return false
	}

	f.pressed = buttonNone

	_, b := f.buttonAt(screen)
	f.hover(screen)

	if b != pressed {
		return true
	}

	switch pressed {
	case buttonMinimize:
		f.sysCommand(scMinimize)

	case buttonMaximize:
		if f.maximized {
			f.sysCommand(scRestore)
		} else {
			f.sysCommand(scMaximize)
		}

	case buttonClose:
		f.sysCommand(scClose)
	}

	return true
}

func (f *frame) sysCommand(cmd uintptr) {
	procPostMessage.Call(f.hwnd, wmSysCommand, cmd, 0)
}

func (f *frame) windowRect() rect {
	var r rect
	procGetWindowRect.Call(f.hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func (r rect) contains(p point) bool {
	return p.X >= r.Left && p.X < r.Right && p.Y >= r.Top && p.Y < r.Bottom
}

func (f *frame) destroy() {
	frameMu.Lock()
	delete(frames, f.hwnd)
	frameMu.Unlock()

	if f.controls != nil {
		f.controls.destroy()
	}

	if f.menu != nil {
		f.menu.destroy()
	}
}

func frameProc(hwnd, msg, wp, lp uintptr) uintptr {
	f := lookupFrame(hwnd)

	if f == nil {
		r, _, _ := procDefWindowProc.Call(hwnd, msg, wp, lp)
		return r
	}

	switch msg {
	case wmNCCalcSize:
		if wp != 0 {
			// Let the original procedure compute the frame — it is the only way to
			// get a maximized window's rect right, since that has to account for
			// the work area and an auto-hiding taskbar — then take back just the
			// top, so the client area reaches the top of the window. The side and
			// bottom borders stay non-client and keep working natively.
			r, _, _ := procCallWindowProc.Call(f.original, hwnd, msg, wp, lp)

			params := (*ncCalcSizeParams)(unsafe.Pointer(lp))
			params.Rgrc[0].Top -= f.captionHeight()

			// A maximized window's rect already extends past the work area by the
			// border thickness, so only the caption is reclaimed there; reclaiming
			// the border too would push content off-screen.
			if !isZoomed(hwnd) {
				params.Rgrc[0].Top -= f.topBorder()
			}

			return r
		}

	case wmNCHitTest:
		if result, ok := f.hitTest(point{X: loWord(lp), Y: hiWord(lp)}); ok {
			return result
		}

	case wmNCMouseMove:
		f.hover(point{X: loWord(lp), Y: hiWord(lp)})

	case wmNCMouseLeave:
		f.clearHover()

	case wmNCLButtonDown, wmNCLButtonDblClk:
		// DefWindowProc would draw the legacy caption buttons for these codes, so
		// the presses we claimed have to be swallowed. A double-click is treated as
		// a press so holding the button down does not fall through to the caption
		// behaviour.
		if f.press(point{X: loWord(lp), Y: hiWord(lp)}) {
			return 0
		}

	case wmNCLButtonUp:
		if f.release(point{X: loWord(lp), Y: hiWord(lp)}) {
			return 0
		}

	case wmSize, wmDpiChanged:
		f.sync()

	case wmThemeChanged, wmSettingChange:
		f.setDark(darkMode())
	}

	r, _, _ := procCallWindowProc.Call(f.original, hwnd, msg, wp, lp)
	return r
}
