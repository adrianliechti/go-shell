//go:build windows

package shell

import (
	"fmt"
	"sync"

	webview2 "github.com/jchv/go-webview2"
)

// titleBarInsets is the geometry a page has to keep clear for the window controls
// the shell draws itself, in CSS pixels. It reaches the page as CSS custom
// properties on the document element, so a layout can reserve room for them
// without knowing the platform.
type titleBarInsets struct {
	Height    int  `json:"height"`
	Left      int  `json:"left"`
	Right     int  `json:"right"`
	Maximized bool `json:"maximized"`
}

// A navigation replaces the document, so each fresh page pulls the frame's
// current geometry into the environment object installed at document start.
const titleBarPullScript = `__shellTitleBar().then(window.__shellApplyTitleBarInsets);`

// titleBarPushScript is the same assignment for a document that is already
// loaded, evaluated whenever the insets change.
func titleBarPushScript(insets titleBarInsets) string {
	return fmt.Sprintf(
		`window.__shellApplyTitleBarInsets?.({height:%d,left:%d,right:%d,maximized:%t});`,
		insets.Height,
		insets.Left,
		insets.Right,
		insets.Maximized,
	)
}

// installOverlay assembles the overlay title bar: the custom frame with its
// native buttons, the drag bridge for the web-content part of the strip, the
// insets the page lays itself out against.
func installOverlay(w webview2.WebView, hwnd uintptr, opts Options) (*frame, error) {
	var (
		mu      sync.Mutex
		current titleBarInsets
	)

	// The frame calls publish from the UI thread whenever the strip moves.
	publish := func(next titleBarInsets) {
		mu.Lock()
		current = next
		mu.Unlock()

		w.Eval(titleBarPushScript(next))
	}

	f, err := newFrame(hwnd, opts, darkMode(), publish)

	if err != nil {
		return nil, err
	}

	bindWindowDrag(w, hwnd)

	w.Bind("__shellTitleBar", func() titleBarInsets {
		mu.Lock()
		defer mu.Unlock()

		return current
	})

	w.Init(titleBarPullScript)

	return f, nil
}
