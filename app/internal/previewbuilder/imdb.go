package previewbuilder

import (
	"net/url"
	"regexp"
	"strings"
)

var imdbTitleIDPattern = regexp.MustCompile(`^tt[0-9]+$`)

func imdbTitleID(rawURL string) (string, bool) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	hostname := strings.ToLower(parsedURL.Hostname())
	if hostname != "imdb.com" && !strings.HasSuffix(hostname, ".imdb.com") {
		return "", false
	}

	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	for i := 0; i+1 < len(pathParts); i++ {
		if strings.EqualFold(pathParts[i], "title") && imdbTitleIDPattern.MatchString(pathParts[i+1]) {
			return pathParts[i+1], true
		}
	}
	return "", false
}
