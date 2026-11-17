// Package shell opens a native desktop window hosting the platform web view
// (WKWebView on macOS, WebView2 on Windows 11) — a minimal shell for local
// web apps.
//
// It is intentionally small: a window — more of them with NewWindow — with
// narrowly scoped events for native shell commands and window dragging. The
// hosted app otherwise talks to its backend over HTTP and WebSocket. Links
// leaving the app's origin open in the default browser.
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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
	// window; it lives as long as the app.
	Handler http.Handler

	// OnShutdown runs once when the user closes the last window or quits the
	// app, before Run returns. On macOS the native event loop remains
	// responsive while the callback runs.
	OnShutdown func()

	// Window size in logical pixels; defaults to 1280x800.
	Width  int
	Height int

	// Minimum window size; unset means the platform default.
	MinWidth  int
	MinHeight int

	// Debug enables the web inspector (macOS) / devtools (Windows).
	Debug bool

	// Microphone lets pages from the app's own origin capture audio with
	// getUserMedia. Without it, and for cameras and other origins, media
	// capture is denied. macOS bundles also need a usage description, which
	// appbundle adds with -microphone.
	Microphone bool

	// TitleBar controls native title-bar presentation. Overlay mode lets web
	// content occupy the title-bar area and enables regions marked with the CSS
	// custom property "--shell-window-drag: drag" to move the window. The window
	// controls stay native; the space to reserve for them is published to the
	// page through window.shell and as CSS custom properties on the document
	// element.
	TitleBar TitleBarOptions

	// FileMenu adds application commands to the native File menu. Selecting
	// one dispatches a "shell:command" CustomEvent on window whose detail is
	// the item's Command. Key is a macOS key equivalent with Command implied.
	// FileMenu is native on macOS; Windows applications should place equivalent
	// commands in their web title bar.
	FileMenu []MenuItem

	shutdown func()
}

func (opts *Options) prepareShutdown() {
	if opts.OnShutdown == nil {
		return
	}

	callback := opts.OnShutdown
	var once sync.Once
	opts.shutdown = func() {
		once.Do(callback)
	}
}

// Run opens the window and blocks until the last window is closed (or the app
// is quit), then returns so deferred cleanup can run. It must be called from
// the main goroutine.
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

	var serveErr chan error

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
		// handler may expose privileged APIs. Each window exchanges the
		// per-run token for a session cookie on its first navigation.
		secret, err := token()

		if err != nil {
			return err
		}

		serveErr = make(chan error, 1)

		go func() {
			serveErr <- http.Serve(ln, protect(secret, opts.Handler))
		}()

		opts.URL = fmt.Sprintf("http://127.0.0.1:%d/?%s=%s", ln.Addr().(*net.TCPAddr).Port, tokenParam, secret)

	case opts.URL == "":
		return errors.New("shell: URL or Handler is required")
	}

	opts.prepareShutdown()
	running.Store(true)
	err := run(opts)
	running.Store(false)
	if opts.shutdown != nil {
		opts.shutdown()
	}

	if err != nil {
		return err
	}

	// Surface a server that died while the window was open — the UI would
	// have appeared dead with no hint why.
	if serveErr != nil {
		select {
		case err := <-serveErr:
			return fmt.Errorf("shell: server failed: %w", err)
		default:
		}
	}

	return nil
}

var running atomic.Bool

var errWindowNotRunning = errors.New("shell: window is not running")

// tokenParam carries the per-run token on a window's first navigation.
const tokenParam = "shell_token"

// PickFolder opens the native directory picker on the shell window and
// returns the chosen path, or "" if the user cancels. It must be called
// while Run is active and never from the main goroutine (which is busy
// running the event loop) — an HTTP handler of the hosted app is the
// natural place.
func PickFolder(title string) (string, error) {
	if !running.Load() {
		return "", errWindowNotRunning
	}

	return pickFolder(title)
}

// NewWindow opens another window of the running app. The window loads the
// address the first one opened — Options.URL, or the Handler's root — with
// params added to its query. Supplied keys replace existing values. The shell
// reserves shell_token. NewWindow returns when the native window opens, or
// returns an error if the app is shutting down. Like PickFolder, it must be
// called while Run is active and never from the main goroutine. Windows
// returns errors.ErrUnsupported.
func NewWindow(params map[string]string) error {
	if !running.Load() {
		return errWindowNotRunning
	}

	return newWindow(params)
}

// windowURL adds params to the query of the start address.
func windowURL(start string, params map[string]string) (string, error) {
	if len(params) == 0 {
		return start, nil
	}
	if _, reserved := params[tokenParam]; reserved {
		return "", fmt.Errorf("shell: %s is reserved", tokenParam)
	}

	u, err := url.Parse(start)

	if err != nil {
		return "", err
	}

	// Keep existing fields verbatim, including values with literal semicolons
	// and repeated keys. Decode only the keys we need to compare.
	var fields []string
	if u.RawQuery != "" {
		for field := range strings.SplitSeq(u.RawQuery, "&") {
			rawKey, _, _ := strings.Cut(field, "=")
			key, err := url.QueryUnescape(rawKey)
			if _, replaced := params[key]; err != nil || !replaced {
				fields = append(fields, field)
			}
		}
	}

	added := url.Values{}
	for key, value := range params {
		added.Set(key, value)
	}

	u.RawQuery = strings.Join(append(fields, added.Encode()), "&")
	return u.String(), nil
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
	// Cookie identity does not include the port, so shared WebView profiles
	// need a per-run name. Hashing keeps the bearer token out of the name.
	cookieName := fmt.Sprintf("shell_session_%x", sha256.Sum256([]byte(secret)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Additional windows may already have the cookie on their first request.
		if query := r.URL.Query(); equal(query.Get(tokenParam), secret) {
			http.SetCookie(w, &http.Cookie{
				Name:  cookieName,
				Value: secret,

				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})

			query.Del(tokenParam)

			target := *r.URL
			target.RawQuery = query.Encode()

			http.Redirect(w, r, target.String(), http.StatusSeeOther)
			return
		}

		if cookie, err := r.Cookie(cookieName); err == nil && equal(cookie.Value, secret) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

func equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
