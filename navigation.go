package shell

import (
	"net/url"
	"strings"
)

// isAppNavigation compares origins, including the effective port. Blob URLs
// retain their creator's origin so in-app blob downloads stay in the WebView.
func isAppNavigation(base *url.URL, rawURL string) bool {
	target, err := url.Parse(rawURL)
	if err != nil || base == nil {
		return false
	}
	if target.Scheme == "blob" {
		target, err = url.Parse(target.Opaque)
		if err != nil {
			return false
		}
	}
	return target.Scheme != "" &&
		strings.EqualFold(base.Scheme, target.Scheme) &&
		strings.EqualFold(base.Hostname(), target.Hostname()) &&
		originPort(base) == originPort(target)
}

func originPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
