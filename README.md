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

## Packaging tools

An app ships a single `appicon.png` (square, ideally 1024x1024); everything
platform-specific is generated from it. Both tools are meant to be pinned as
[tool dependencies](https://go.dev/doc/modules/managing-dependencies#tools):

```
tool (
	github.com/adrianliechti/go-shell/cmd/appbundle
	github.com/adrianliechti/go-shell/cmd/winres
)
```

- `go tool winres -name App -description "..." -company "..." -in appicon.png`
  — generates the Windows resource objects (`rsrc_windows_*.syso`: icon,
  per-monitor-v2 DPI manifest, version info). Generate and commit them via a
  `go:generate` directive next to the main package; `go build` links them in
  and the window picks up the icon (`RT_GROUP_ICON` `#1`).
- `go tool appbundle -name App -id com.example.app -package ./app -version 1.2.3`
  — compiles the main package and assembles `App.app`: binary, generated
  Info.plist, `icon.icns` rendered from `appicon.png`, ad-hoc code signature.
