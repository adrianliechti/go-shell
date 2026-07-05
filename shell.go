// Package shell opens a native desktop window hosting the platform web view
// (WKWebView on macOS, WebView2 on Windows 11) — a minimal shell for local
// web apps.
//
// It is intentionally small: a single window and no JavaScript bridge — the
// hosted app talks to its backend over HTTP and WebSocket. Links leaving the
// app's origin open in the default browser.
//
// Serve an http.Handler in a window:
//
//	shell.Run(shell.Options{Title: "App", Handler: mux})
//
// or point the window at an already-running URL:
//
//	shell.Run(shell.Options{Title: "App", URL: "http://127.0.0.1:8080"})
package shell

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
)

func init() {
	// The Cocoa / Win32 event loop must run on the process' main thread.
	runtime.LockOSThread()
}

type Options struct {
	Title string

	// URL is loaded in the window. Mutually exclusive with Handler.
	URL string

	// Handler is served on an ephemeral loopback listener and loaded in the
	// window; it lives for the lifetime of the window.
	Handler http.Handler

	// Window size in logical pixels; defaults to 1280x800.
	Width  int
	Height int

	// Minimum window size; unset means the platform default. Only enforced
	// on macOS — WebView2 windows are freely resizable.
	MinWidth  int
	MinHeight int

	// Debug enables the web inspector (macOS) / devtools (Windows).
	Debug bool
}

// Run opens the window and blocks until it is closed. It must be called from
// the main goroutine. On macOS, quitting the app terminates the process
// before Run returns.
func Run(opts Options) error {
	if opts.Title == "" {
		return errors.New("shell: Title is required")
	}

	if opts.Width <= 0 {
		opts.Width = 1280
	}

	if opts.Height <= 0 {
		opts.Height = 800
	}

	switch {
	case opts.Handler != nil && opts.URL != "":
		return errors.New("shell: URL and Handler are mutually exclusive")

	case opts.Handler != nil:
		ln, err := net.Listen("tcp", "127.0.0.1:0")

		if err != nil {
			return err
		}

		defer ln.Close()

		// The loopback port is reachable by every process on the machine and,
		// via CSRF or DNS rebinding, by web pages in local browsers — and the
		// handler may expose privileged APIs. Only this window knows the
		// per-run token; the first navigation exchanges it for a session
		// cookie, everything else is rejected.
		secret, err := token()

		if err != nil {
			return err
		}

		go http.Serve(ln, protect(secret, opts.Handler))

		opts.URL = fmt.Sprintf("http://127.0.0.1:%d/?shell_token=%s", ln.Addr().(*net.TCPAddr).Port, secret)

	case opts.URL == "":
		return errors.New("shell: URL or Handler is required")
	}

	return run(opts)
}

func token() (string, error) {
	b := make([]byte, 32)

	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

// protect only lets requests through that carry the session cookie. A request
// presenting the token (the window's initial navigation) is redirected to the
// same URL without it — so it never lingers in the address bar or history —
// with the cookie set. HttpOnly keeps it from scripts; SameSite=Strict keeps
// browsers from attaching it to cross-site requests.
func protect(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("shell_session"); err == nil && equal(cookie.Value, secret) {
			next.ServeHTTP(w, r)
			return
		}

		if query := r.URL.Query(); equal(query.Get("shell_token"), secret) {
			http.SetCookie(w, &http.Cookie{
				Name:  "shell_session",
				Value: secret,

				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})

			query.Del("shell_token")

			target := *r.URL
			target.RawQuery = query.Encode()

			http.Redirect(w, r, target.String(), http.StatusSeeOther)
			return
		}

		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
