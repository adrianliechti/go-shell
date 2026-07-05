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
	})

	if err != nil {
		log.Fatal(err)
	}
}
```

`shell.Run` must be called from the main goroutine (the native event loop
runs on the main thread) and blocks until the window closes.

The window is a complete little browser shell: native JavaScript dialogs
(`alert`/`confirm`/`prompt`), file pickers (`<input type="file">`), downloads
to the Downloads folder (server attachments as well as blob/`download`
anchors), page zoom, reload, and a menu bar / dark-mode aware chrome. Try it:

```
cd example && task run
```

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
