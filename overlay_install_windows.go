//go:build windows

package shell

import (
	"encoding/json"
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

// titleBarPullScript applies the insets at document start. The frame pushes them
// whenever the strip moves, but a navigation replaces the document element the
// properties live on, so a fresh document pulls the current values rather than
// carrying whatever was captured when the script was injected.
//
// It runs at document-start, before <html> is guaranteed to exist, so the marker
// is applied through a helper that also re-applies once the document element is
// available. Without this, the page can render before the marker lands and the
// window chrome only appears after some later reflow (e.g. a resize).
const titleBarPullScript = `(() => {
	const apply = (insets) => {
		const el = document.documentElement;
		if (!el) return;
		el.dataset.windowChrome = 'windows-overlay';
		if (insets) {
			el.style.setProperty('--shell-titlebar-height', insets.height + 'px');
			el.style.setProperty('--shell-titlebar-inset-left', insets.left + 'px');
			el.style.setProperty('--shell-titlebar-inset-right', insets.right + 'px');
			el.dataset.shellWindow = insets.maximized ? 'maximized' : 'normal';
		}
	};
	apply();
	if (!document.documentElement) {
		document.addEventListener('readystatechange', () => apply(), { once: true });
	}
	__shellTitleBar().then(apply);
})();`

// titleBarPushScript is the same assignment for a document that is already
// loaded, evaluated whenever the insets change.
func titleBarPushScript(insets titleBarInsets) string {
	maximized := "normal"

	if insets.Maximized {
		maximized = "maximized"
	}

	return fmt.Sprintf(`(() => {
	document.documentElement.dataset.windowChrome = 'windows-overlay';
	const style = document.documentElement.style;
	style.setProperty('--shell-titlebar-height', '%dpx');
	style.setProperty('--shell-titlebar-inset-left', '%dpx');
	style.setProperty('--shell-titlebar-inset-right', '%dpx');
	document.documentElement.dataset.shellWindow = '%s';
})();`, insets.Height, insets.Left, insets.Right, maximized)
}

// installOverlay assembles the overlay title bar: the custom frame with its
// native buttons, the drag bridge for the web-content part of the strip, the
// FileMenu popup, and the insets the page lays itself out against.
func installOverlay(w webview2.WebView, hwnd uintptr, opts Options) (*frame, error) {
	var (
		mu      sync.Mutex
		current titleBarInsets
		enabled = map[string]bool{}
	)

	for _, item := range opts.FileMenu {
		if item.Separator || item.Command == "" {
			continue
		}

		enabled[item.Command] = !item.Disabled
	}

	// The frame calls publish from the UI thread whenever the strip moves.
	publish := func(next titleBarInsets) {
		mu.Lock()
		current = next
		mu.Unlock()

		w.Eval(titleBarPushScript(next))
	}

	dispatch := func(command string) {
		detail, err := json.Marshal(command)

		if err != nil {
			return
		}

		w.Eval(fmt.Sprintf("window.dispatchEvent(new CustomEvent('shell:command',{detail:%s}));", detail))
	}

	// popup runs on the UI thread, from the menu button's click.
	var f *frame

	popup := func() {
		mu.Lock()
		state := make(map[string]bool, len(enabled))

		for command, on := range enabled {
			state[command] = on
		}

		mu.Unlock()

		showFileMenu(hwnd, f.menuAnchor(), opts.FileMenu, state, dispatch)
	}

	f, err := newFrame(hwnd, opts, darkMode(), popup, publish)

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

	// Mirror the macOS shell:command-state contract so the page can grey out menu
	// items it cannot currently service.
	w.Bind("__shellCommandState", func(command string, on bool) {
		mu.Lock()
		enabled[command] = on
		mu.Unlock()
	})

	w.Init(`window.addEventListener('shell:command-state', (event) => {
		const state = event.detail;
		if (!state || typeof state.command !== 'string' || typeof state.enabled !== 'boolean') return;
		__shellCommandState(state.command, state.enabled);
	});`)

	return f, nil
}
