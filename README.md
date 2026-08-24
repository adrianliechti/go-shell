# go-shell

A minimal desktop shell for local web apps: one native window hosting the
platform web view — WKWebView on macOS, WebView2 on Windows 11. No JavaScript
bridge, no framework; the hosted app talks to its backend over plain HTTP and
WebSocket. Links leaving the app's origin open in the default browser.

## Usage

```go
package main

import (
	"log"
	"net/http"

	shell "github.com/adrianliechti/go-shell"
)

func main() {
	var handler http.Handler // your app

	err := shell.Run(shell.Options{
		Title:   "App",
		Handler: handler, // served on a loopback listener; or set URL instead

		Width:  1280,
		Height: 800,
		OnShutdown: func() {
			// Stop background work and close child processes.
		},

		TitleBar: shell.TitleBarOptions{Overlay: true},
		FileMenu: []shell.MenuItem{
			{Title: "New File...", Command: "new-file", Key: "n"},
			{Title: "Save", Command: "save", Key: "s", Disabled: true},
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
```

`shell.Run` must be called from the main goroutine (the native event loop
runs on the main thread) and blocks until the window closes.

`OnShutdown`, when set, runs exactly once after the user closes the window or
quits the app and before `Run` returns. On macOS, the native event loop remains
responsive while the callback runs, so bounded backend cleanup does not make
the app appear hung while it is quitting.

The window is a complete little browser shell: native JavaScript dialogs
(`alert`/`confirm`/`prompt`), file pickers (`<input type="file">`), downloads
to the Downloads folder (server attachments as well as blob/`download`
anchors), page zoom, reload, and a menu bar / dark-mode aware chrome. Try it:

```
cd example && task run
```

The one native call the browser cannot make itself — choosing a *directory*
and learning its path — is exposed as a plain function the backend can call
from a request handler (never from the main goroutine):

```go
path, err := shell.PickFolder("Open Workspace") // "" if cancelled
```

`TitleBar.Overlay` lets the page draw where the title bar would be, so a tab
strip or toolbar can sit in the window chrome itself. Mark drag regions in CSS
while leaving interactive children unmarked:

```css
.titlebar { --shell-window-drag: drag; }
.titlebar button { --shell-window-drag: no-drag; }
```

Dragging a marked region moves the window and double-clicking it maximizes or
restores. On Windows, right-clicking it also opens the window's system menu.

The window controls retain their platform behaviour — on macOS the traffic
lights float over the content (nudge them inward with
`ControlsOffsetX`/`ControlsOffsetY`), and on Windows the shell draws minimize,
maximize/restore and close at the trailing edge, including the Windows 11 Snap
Layouts flyout. Because they sit inside the page's layout, the shell publishes
their measured bounds plus a small safety gutter as CSS custom properties on the
document element:

```css
.titlebar {
  height: var(--shell-titlebar-height, 40px);
  padding-left: var(--shell-titlebar-inset-left, 0px);
  padding-right: var(--shell-titlebar-inset-right, 0px);
}
```

On macOS the left inset is populated; on Windows the right inset is populated.
They update on resize, maximize/full-screen and scale/DPI changes;
`html[data-shell-window]` is `maximized` or `normal`, and
`html[data-window-chrome]` identifies `macos-overlay` or `windows-overlay`.
`TitleBar.Height` overrides the default platform strip height.

The same information is available without URL flags through the read-only
JavaScript host object, which is injected at document start:

```js
window.shell.platform                 // "macos" or "windows"
window.shell.titleBar.overlay         // true when content enters the title bar
window.shell.titleBar.height          // current height in CSS pixels
window.shell.titleBar.insets.left     // macOS controls + safety gutter
window.shell.titleBar.insets.right    // Windows controls + safety gutter
window.shell.titleBar.maximized

window.addEventListener("shell:titlebar-change", ({ detail }) => {
  layoutTitleBar(detail.insets);
});
```

The CSS properties and data attributes mirror this object for layouts that do
not need JavaScript.

`FileMenu` prepends application commands to the native File menu. A selection
dispatches a `shell:command` event in the page:

```js
window.addEventListener("shell:command", (event) => {
  if (event.detail === "save") saveCurrentDocument();
});

window.dispatchEvent(new CustomEvent("shell:command-state", {
  detail: { command: "save", enabled: hasUnsavedChanges },
}));
```

On macOS, `Key` uses Command as the primary modifier; `Shift`, `Alt`, and
`Control` add modifiers. `Disabled` sets the initial state; publish a
`shell:command-state` event when it changes. Use `{Separator: true}` to group
commands. Windows applications own their title-bar menu and keyboard commands;
the published insets let that UI avoid the native window controls.

With `Handler`, the loopback listener is guarded by a per-run random token:
the window's first navigation exchanges it for an HttpOnly, SameSite=Strict
session cookie, and requests without it are rejected with 401. Other local
processes and web pages in browsers (CSRF, DNS rebinding) cannot reach the
handler — but this also means the app's URL is not usable in an outside
browser. With `URL`, protecting the server is the caller's responsibility.

## Packaging

An app ships a single `appicon.png` (square, ideally 1024x1024) next to its
main package; everything platform-specific is generated from it at build time
by one tool, meant to be pinned as a
[tool dependency](https://go.dev/doc/modules/managing-dependencies#tools):

```
tool github.com/adrianliechti/go-shell/cmd/appbundle
```

```
go tool appbundle -name App -id com.example.app -description "..." -company "..." -copyright "..." -package ./app -version 1.2.3
```

- On macOS it assembles `App.app`: binary, generated Info.plist, `icon.icns`
  rendered from the PNG, ad-hoc code signature.
- On Windows (or with `-os windows`) it builds `App.exe` (windowsgui): a
  resource object — icon (`RT_GROUP_ICON` `#1`, used by the window),
  per-monitor-v2 DPI manifest, version info — is generated next to the main
  package for the duration of the build and removed afterwards. Note that a
  plain `go build` without the tool yields an exe without icon or manifest.
