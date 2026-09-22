package urlutil

import (
	"errors"
	"html"
	"net/url"
	"strings"
)

// NormalizeHTTPURL makes a URL detected in message text safe to use as an
// HTTP link. Scheme-less URLs are interpreted as HTTPS.
func NormalizeHTTPURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(html.UnescapeString(rawURL))
	lowerURL := strings.ToLower(rawURL)

	if strings.HasPrefix(rawURL, "//") {
		rawURL = "https:" + rawURL
	} else if !strings.HasPrefix(lowerURL, "http://") &&
		!strings.HasPrefix(lowerURL, "https://") {
		rawURL = "https://" + rawURL
	}

	parsedURL, err := url.ParseRequestURI(rawURL)
	if err != nil || parsedURL.Host == "" {
		return "", errors.New("invalid HTTP URL")
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") &&
		!strings.EqualFold(parsedURL.Scheme, "https") {
		return "", errors.New("unsupported URL scheme")
	}

	return parsedURL.String(), nil
}
