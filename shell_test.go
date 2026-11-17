package shell

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPrepareShutdownRunsCallbackOnce(t *testing.T) {
	calls := 0
	opts := Options{OnShutdown: func() { calls++ }}
	opts.prepareShutdown()

	opts.shutdown()
	opts.shutdown()

	if calls != 1 {
		t.Fatalf("shutdown callback ran %d times, want 1", calls)
	}
}

func TestPrepareShutdownWithoutCallback(t *testing.T) {
	opts := Options{}
	opts.prepareShutdown()

	if opts.shutdown != nil {
		t.Fatal("prepared a shutdown callback for nil OnShutdown")
	}
}

func TestProtect(t *testing.T) {
	secret, err := token()

	if err != nil {
		t.Fatal(err)
	}

	handler := protect(secret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))

	// No credentials — what any other local process or a browser page sees.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no credentials: got %d, want 401", rec.Code)
	}

	// Wrong token.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/?shell_token=wrong", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: got %d, want 401", rec.Code)
	}

	// The window's initial navigation: token is exchanged for a cookie and
	// stripped from the URL.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/?shell_token="+secret, nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("token exchange: got %d, want 303", rec.Code)
	}

	if location := rec.Header().Get("Location"); location != "/" {
		t.Fatalf("token exchange: got location %q, want /", location)
	}

	cookies := rec.Result().Cookies()

	if len(cookies) != 1 || !strings.HasPrefix(cookies[0].Name, "shell_session_") {
		t.Fatalf("token exchange: expected a per-instance shell_session cookie, got %v", cookies)
	}

	if strings.Contains(cookies[0].Name, secret) {
		t.Fatal("token exchange: cookie name exposes the bearer token")
	}

	if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("token exchange: cookie must be HttpOnly and SameSite=Strict")
	}

	// Follow-up request with the session cookie.
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookies[0])

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("with cookie: got %d %q, want 200 ok", rec.Code, rec.Body.String())
	}

	// Wrong cookie.
	req = httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: cookies[0].Name, Value: "wrong"})

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong cookie: got %d, want 401", rec.Code)
	}
}

func TestShellEnvironmentScriptPublishesHostAndInsets(t *testing.T) {
	script := shellEnvironmentScript("windows", true)

	for _, want := range []string{
		`const platform = "windows"`,
		"const overlay = true",
		"Object.defineProperty(window, 'shell'",
		"insets: { left: state.left, right: state.right }",
		"'shell:titlebar-change'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("environment script does not contain %q", want)
		}
	}
}

// Windows WebView2 instances share a profile, and cookies ignore port numbers.
func TestProtectSharedCookieStore(t *testing.T) {
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	first := httptest.NewServer(protect("first-instance-secret", app))
	defer first.Close()
	second := httptest.NewServer(protect("second-instance-secret", app))
	defer second.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	get := func(target string) []*http.Cookie {
		t.Helper()
		resp, err := client.Get(target)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: got %d, want 200", target, resp.StatusCode)
		}
		if resp.Request.URL.Query().Has("shell_token") {
			t.Fatal("bootstrap token was not removed from the URL")
		}
		return jar.Cookies(resp.Request.URL)
	}

	firstCookies := get(first.URL + "/?shell_token=first-instance-secret")
	if len(firstCookies) != 1 {
		t.Fatalf("first bootstrap: got %d cookies, want 1", len(firstCookies))
	}
	get(second.URL + "/?shell_token=second-instance-secret")

	// Both instances must stay authenticated after either bootstrap.
	get(first.URL)
	get(second.URL)

	// A cookie issued by one instance must not authenticate another.
	req, err := http.NewRequest(http.MethodGet, second.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(firstCookies[0])
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("other instance's cookie: got %d, want 401", resp.StatusCode)
	}
}

func TestWindowURLAddsParamsToTheStartAddress(t *testing.T) {
	got, err := windowURL("http://127.0.0.1:1234/?shell_token=secret", map[string]string{"project": "/tmp/a b", "chat": "1"})

	if err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(got)

	if err != nil {
		t.Fatal(err)
	}

	query := u.Query()

	if u.Host != "127.0.0.1:1234" || u.Path != "/" || query.Get("shell_token") != "secret" || query.Get("project") != "/tmp/a b" || query.Get("chat") != "1" {
		t.Fatalf("window URL = %s", got)
	}
}

func TestWindowURLKeepsTheStartAddressWithoutParams(t *testing.T) {
	const start = "http://localhost:5173/app/?mode=dev"

	if got, err := windowURL(start, nil); err != nil || got != start {
		t.Fatalf("window URL = %q, %v", got, err)
	}
}

func TestWindowURLPreservesUnchangedQueryFields(t *testing.T) {
	const start = "http://localhost/app?theme=blue;red&tag=a%20b&tag=c&flag&%64oc=old&doc=older#page"
	const want = "http://localhost/app?theme=blue;red&tag=a%20b&tag=c&flag&doc=new%2Ffile#page"
	got, err := windowURL(start, map[string]string{"doc": "new/file"})
	if err != nil || got != want {
		t.Fatalf("window URL = %q, %v; want %q", got, err, want)
	}
}

func TestWindowURLRefusesTheToken(t *testing.T) {
	if _, err := windowURL("http://127.0.0.1:1/?shell_token=a", map[string]string{"shell_token": "b"}); err == nil {
		t.Fatal("a window param replaced the session token")
	}
}

func TestNewWindowNeedsTheShellRunning(t *testing.T) {
	if err := NewWindow(nil); err == nil {
		t.Fatal("NewWindow opened a window before Run")
	}
}

func TestProtectKeepsTheParamsOfANewWindow(t *testing.T) {
	secret, err := token()

	if err != nil {
		t.Fatal(err)
	}

	handler := protect(secret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest("GET", "/?shell_token="+secret, nil))
	cookies := first.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("first window did not receive a session cookie")
	}

	address, err := windowURL("http://127.0.0.1:1/?shell_token="+secret, map[string]string{"project": "/tmp/a"})

	if err != nil {
		t.Fatal(err)
	}

	u, err := url.Parse(address)

	if err != nil {
		t.Fatal(err)
	}

	for _, cookie := range []*http.Cookie{nil, cookies[0]} {
		req := httptest.NewRequest("GET", u.RequestURI(), nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if location := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || location != "/?project=%2Ftmp%2Fa" {
			t.Errorf("existing cookie %t: got %d to %q, want 303 to the window's params", cookie != nil, rec.Code, location)
		}
	}
}
