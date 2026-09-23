package previewbuilder

import (
	"html"
	"regexp"
)

var redditTitlePattern = regexp.MustCompile(`<shreddit-title title="([^"]+)">`)

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
