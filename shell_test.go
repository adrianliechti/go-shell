package shell

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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

	if len(cookies) != 1 || cookies[0].Name != "shell_session" {
		t.Fatalf("token exchange: expected a shell_session cookie, got %v", cookies)
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
	req.AddCookie(&http.Cookie{Name: "shell_session", Value: "wrong"})

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong cookie: got %d, want 401", rec.Code)
	}
}
