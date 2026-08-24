//go:build windows

package shell

import webview2 "github.com/jchv/go-webview2"

// The WebView2 widget is a child window covering the whole client area, so the
// title-bar strip is web content and every pointer event in it goes to the page,
// not to the frame. Dragging, double-click-to-maximize and the system menu are
// therefore driven from the page: an injected script watches for presses in
// regions marked "--shell-window-drag: drag" — the same CSS contract as macOS —
// and calls back into Go, which hands the gesture to Windows.

// windowDragScript mirrors the macOS drag script. The pointer position is passed
// along because a synthesized WM_NCLBUTTONDOWN needs a screen coordinate, and
// the page's own coordinates are the only ones available at that point.
const windowDragScript = `(() => {
	if (window.__shellWindowDragInstalled) return;
	window.__shellWindowDragInstalled = true;

	const isDragRegion = (target) =>
		target instanceof Element &&
		getComputedStyle(target).getPropertyValue('--shell-window-drag').trim() === 'drag';

	document.addEventListener('mousedown', (event) => {
		if (event.button !== 0 || !isDragRegion(event.target)) return;
		event.preventDefault();
		// A double-click toggles maximize, matching a native caption.
		if (event.detail === 2) {
			__shellWindowMaximizeToggle();
			return;
		}
		__shellWindowDrag(event.screenX, event.screenY);
	}, true);

	document.addEventListener('contextmenu', (event) => {
		if (!isDragRegion(event.target)) return;
		event.preventDefault();
		__shellWindowSystemMenu(event.screenX, event.screenY);
	}, true);
})();`

// bindWindowDrag exposes the callbacks windowDragScript uses. MouseEvent's
// screenX/screenY are already in physical screen pixels for a WebView2 window at
// the shell's per-monitor-v2 DPI awareness, so they can be used as-is.
func bindWindowDrag(w webview2.WebView, hwnd uintptr) {
	w.Bind("__shellWindowDrag", func(x, y float64) {
		beginWindowDrag(hwnd, point{X: int32(x), Y: int32(y)})
	})

	w.Bind("__shellWindowMaximizeToggle", func() {
		cmd := uintptr(scMaximize)

		if isZoomed(hwnd) {
			cmd = scRestore
		}

		procPostMessage.Call(hwnd, wmSysCommand, cmd, 0)
	})

	w.Bind("__shellWindowSystemMenu", func(x, y float64) {
		showSystemMenu(hwnd, point{X: int32(x), Y: int32(y)})
	})

	w.Init(windowDragScript)
}

// beginWindowDrag hands the press to Windows as a caption drag, which is what
// gives the gesture everything a native title bar has: the snap layouts, the
// drag-to-unmaximize, the aero snap preview.
func beginWindowDrag(hwnd uintptr, p point) {
	// The page swallowed the mouse-down, so the button is still physically down
	// while WM_NCLBUTTONDOWN enters its modal move loop — which is exactly what
	// a real caption press looks like.
	procReleaseCapture.Call()
	procPostMessage.Call(hwnd, wmNCLButtonDown, htCaption, uintptr(uint32(p.Y)<<16|uint32(p.X)&0xFFFF))
}

// showSystemMenu opens the window's own system menu, so right-clicking the strip
// offers Restore / Move / Size / Minimize / Maximize / Close as a native caption
// would.
func showSystemMenu(hwnd uintptr, p point) {
	menu, _, _ := procGetSystemMenu.Call(hwnd, 0)

	if menu == 0 {
		return
	}

	procSetForegroundWindow.Call(hwnd)

	cmd, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmReturnCmd|tpmRightButton,
		uintptr(p.X),
		uintptr(p.Y),
		0,
		hwnd,
		0,
	)

	if cmd != 0 {
		procPostMessage.Call(hwnd, wmSysCommand, cmd, 0)
	}
}
