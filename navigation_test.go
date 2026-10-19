package shell

import (
	"net/url"
	"testing"
)

func TestAppNavigationOrigin(t *testing.T) {
	for _, test := range []struct {
		base, target string
		app          bool
	}{
		{"http://127.0.0.1:8080/?shell_token=secret", "http://127.0.0.1:8080/", true},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8081/", false},
		{"http://127.0.0.1:8080", "https://example.com/", false},
		{"https://example.com", "https://EXAMPLE.com:443/path#fragment", true},
		{"http://example.com", "http://example.com:80/", true},
		{"https://example.com", "http://example.com/", false},
		{"https://example.com", "https://example.com.attacker.test/", false},
		{"https://example.com", "https://example.com@attacker.test/", false},
		{"https://example.com", "mailto:hello@example.com", false},
		{"http://127.0.0.1:8080", "blob:http://127.0.0.1:8080/id", true},
		{"http://127.0.0.1:8080", "blob:https://example.com/id", false},
		{"https://example.com", "/relative", false},
		{"https://example.com", ":invalid", false},
	} {
		t.Run(test.target, func(t *testing.T) {
			base, err := url.Parse(test.base)
			if err != nil {
				t.Fatal(err)
			}
			if got := isAppNavigation(base, test.target); got != test.app {
				t.Errorf("isAppNavigation(%q, %q) = %t, want %t", test.base, test.target, got, test.app)
			}
		})
	}
}
