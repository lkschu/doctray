package previewbuilder

import (
	"context"
	"encoding/json"
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

type tmdbFindResponse struct {
	MovieResults []tmdbTitle `json:"movie_results"`
	TVResults    []tmdbTitle `json:"tv_results"`
}

type tmdbTitle struct {
	Title      string `json:"title"`
	Name       string `json:"name"`
	Overview   string `json:"overview"`
	PosterPath string `json:"poster_path"`
}

func (title tmdbTitle) displayTitle() string {
	if title.Title != "" {
		return title.Title
	}
	return title.Name
}

func tmdbPreviewForIMDbTitle(inputURL, apiKey string) (URLPreview, bool, error) {
	imdbID, isIMDbTitle := imdbTitleID(inputURL)
	if !isIMDbTitle {
		return URLPreview{}, false, nil
	}

	endpoint := url.URL{
		Scheme: "https",
		Host:   "api.themoviedb.org",
		Path:   "/3/find/" + imdbID,
	}
	query := endpoint.Query()
	query.Set("api_key", apiKey)
	query.Set("external_source", "imdb_id")
	endpoint.RawQuery = query.Encode()

	response, _, err := fetchPublicURL(context.Background(), "GET", endpoint.String())
	if err != nil {
		return URLPreview{}, true, err
	}
	defer response.Body.Close()

	var result tmdbFindResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return URLPreview{}, true, err
	}

	var title tmdbTitle
	if len(result.MovieResults) > 0 {
		title = result.MovieResults[0]
	} else if len(result.TVResults) > 0 {
		title = result.TVResults[0]
	} else {
		return URLPreview{}, false, nil
	}

	preview := URLPreview{
		URL:         inputURL,
		Title:       StringCleanup(title.displayTitle(), 200),
		Description: StringCleanup(title.Overview, 500),
		Domain:      "www.imdb.com",
	}
	if title.PosterPath != "" {
		preview.Image = "https://image.tmdb.org/t/p/w342" + title.PosterPath
	}
	return preview, true, nil
}
