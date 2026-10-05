package previewbuilder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var youtubeVideoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Only the lookup is canonicalized. Keep timestamps, playlist context and the
// original mobile/short link intact in the resulting preview.
func youtubeVideoURL(rawURL string) (*url.URL, string, bool) {
	pageURL, err := url.Parse(rawURL)
	if err != nil || (pageURL.Scheme != "http" && pageURL.Scheme != "https") || pageURL.User != nil {
		return nil, "", false
	}
	parts := strings.Split(strings.Trim(pageURL.Path, "/"), "/")
	var id string
	switch strings.ToLower(pageURL.Hostname()) {
	case "youtu.be", "www.youtu.be":
		if len(parts) == 1 {
			id = parts[0]
		}
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		if len(parts) == 1 && parts[0] == "watch" {
			id = pageURL.Query().Get("v")
		} else if len(parts) == 2 {
			switch parts[0] {
			case "shorts", "embed", "live":
				id = parts[1]
			}
		}
	}
	if !youtubeVideoIDPattern.MatchString(id) {
		return nil, "", false
	}
	return pageURL, id, true
}

func youtubeOEmbedPreview(ctx context.Context, inputURL string) (URLPreview, bool, error) {
	pageURL, id, handled := youtubeVideoURL(inputURL)
	if !handled {
		return URLPreview{}, false, nil
	}
	endpoint := url.URL{Scheme: "https", Host: "www.youtube.com", Path: "/oembed"}
	query := endpoint.Query()
	query.Set("url", "https://www.youtube.com/watch?v="+id)
	query.Set("format", "json")
	endpoint.RawQuery = query.Encode()
	response, _, err := fetchPublicURL(ctx, http.MethodGet, endpoint.String())
	if err != nil {
		return URLPreview{}, true, err
	}
	defer response.Body.Close()
	preview, err := youtubeOEmbedPreviewFromReader(pageURL, response.Body)
	return preview, true, err
}

func youtubeOEmbedPreviewFromReader(pageURL *url.URL, body io.Reader) (URLPreview, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxPreviewBodyBytes+1))
	if err != nil {
		return URLPreview{}, errors.New("could not read YouTube metadata")
	}
	if len(raw) > maxPreviewBodyBytes {
		return URLPreview{}, errors.New("YouTube metadata exceeds size limit")
	}
	var result struct {
		Type         string `json:"type"`
		Title        string `json:"title"`
		AuthorName   string `json:"author_name"`
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return URLPreview{}, errors.New("invalid YouTube metadata")
	}
	title := strings.TrimSpace(StringCleanup(result.Title, 200))
	image := resolvePreviewURL(pageURL, result.ThumbnailURL)
	if result.Type != "video" || title == "" || image == "" {
		return URLPreview{}, errors.New("incomplete YouTube video metadata")
	}
	author := strings.TrimSpace(StringCleanup(result.AuthorName, 500))
	description := ""
	if author != "" {
		description = StringCleanup("By "+author, 500)
	}
	// oEmbed HTML is intentionally ignored: cards never embed a remote player.
	return URLPreview{
		URL:         pageURL.String(),
		Title:       title,
		Description: description,
		Favicon:     "https://www.youtube.com/favicon.ico",
		Domain:      pageURL.Hostname(),
		Image:       image,
	}, nil
}
