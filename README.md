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

Both are meant to be pinned as [tool dependencies](https://go.dev/doc/modules/managing-dependencies#tools):

```
tool (
	github.com/adrianliechti/go-shell/cmd/appbundle
	github.com/adrianliechti/go-shell/cmd/icns
)
```

- `go tool icns -in appicon.png -out icon.icns` — renders an Apple `.icns`
  from a single square PNG (ideally 1024x1024), replacing the sips/iconutil
  pipeline.
- `go tool appbundle -name App -package ./app -version 1.2.3` — compiles the
  main package and assembles `App.app` (binary, Info.plist template with
  `__VERSION__` stamped, `icon.icns`, ad-hoc code signature).

On Windows, embed the icon and manifest as `.syso` resources instead, e.g.
with [go-winres](https://github.com/tc-hib/go-winres); the window picks up
`RT_GROUP_ICON` `#1`.
