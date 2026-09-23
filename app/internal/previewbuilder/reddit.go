package previewbuilder

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"
)

const redditFaviconURL = "https://www.redditstatic.com/shreddit/assets/favicon/64x64.png"

var redditTitlePattern = regexp.MustCompile(`<shreddit-title title="([^"]+)">`)

type redditOEmbedResponse struct {
	Title string `json:"title"`
}

func redditPostURL(rawURL string) (*url.URL, bool) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, false
	}
	hostname := strings.ToLower(parsedURL.Hostname())
	if hostname != "reddit.com" && !strings.HasSuffix(hostname, ".reddit.com") {
		return nil, false
	}

	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	for i := 0; i+1 < len(pathParts); i++ {
		if strings.EqualFold(pathParts[i], "comments") && pathParts[i+1] != "" {
			return parsedURL, true
		}
	}
	return nil, false
}

func redditSubreddit(postURL *url.URL) string {
	pathParts := strings.Split(strings.Trim(postURL.Path, "/"), "/")
	for i := 0; i+1 < len(pathParts); i++ {
		if strings.EqualFold(pathParts[i], "r") && pathParts[i+1] != "" {
			return "r/" + pathParts[i+1]
		}
	}
	return ""
}

func redditOEmbedPreview(ctx context.Context, inputURL string) (URLPreview, bool) {
	postURL, isRedditPost := redditPostURL(inputURL)
	if !isRedditPost {
		return URLPreview{}, false
	}

	endpoint := url.URL{
		Scheme: "https",
		Host:   "www.reddit.com",
		Path:   "/oembed",
	}
	query := endpoint.Query()
	query.Set("url", postURL.String())
	endpoint.RawQuery = query.Encode()

	response, _, err := fetchPublicURL(ctx, "GET", endpoint.String())
	if err != nil {
		return URLPreview{}, false
	}
	defer response.Body.Close()
	return redditOEmbedPreviewFromReader(postURL, response.Body)
}

func redditOEmbedPreviewFromReader(postURL *url.URL, body io.Reader) (URLPreview, bool) {
	var result redditOEmbedResponse
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return URLPreview{}, false
	}
	title := StringCleanup(result.Title, 200)
	if title == "" {
		return URLPreview{}, false
	}

	return URLPreview{
		URL:         postURL.String(),
		Title:       title,
		Description: redditSubreddit(postURL),
		Favicon:     redditFaviconURL,
		Domain:      postURL.Hostname(),
		Image:       redditFaviconURL,
	}, true
}

func enrichPreview(extraction *previewExtraction, rawHTML string) {
	switch extraction.Preview.Domain {
	case "www.reddit.com":
		enrichReddit(extraction, rawHTML)
	}
}

func enrichReddit(extraction *previewExtraction, rawHTML string) {
	matches := redditTitlePattern.FindAllStringSubmatch(rawHTML, -1)
	if len(matches) < 1 || len(matches[0]) < 2 || matches[0][1] == "" {
		return
	}

	extraction.Preview.Title = StringCleanup(html.UnescapeString(matches[0][1]), 200)
	extraction.TitleSource = "reddit"
}
