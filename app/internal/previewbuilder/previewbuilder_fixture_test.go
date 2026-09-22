package previewbuilder

import (
	"net/url"
	"os"
	"testing"
)

// TestSavedPreviewFixtures is an offline inspection test. Once expected output
// is established, replace the log statements with assertions.
func TestSavedPreviewFixtures(t *testing.T) {
	fixtures := []struct {
		name      string
		filename  string
		sourceURL string
	}{
		{
			name:      "imdb-title",
			filename:  "imdb.html",
			sourceURL: "https://www.imdb.com/de/title/tt0107007/",
		},
		{
			name:      "reddit-overlay-mod",
			filename:  "reddit-overlay_mod.html",
			sourceURL: "https://www.reddit.com/r/MonsterHunterWorld/comments/17xmkrf/overlay_mod",
		},
		{
			name:      "youtube-watch",
			filename:  "youtube-watch.html",
			sourceURL: "https://www.youtube.com/watch?v=AZmql5nbTl0",
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			body, err := os.ReadFile("testdata/" + fixture.filename)
			if err != nil {
				t.Fatal(err)
			}
			pageURL, err := url.Parse(fixture.sourceURL)
			if err != nil {
				t.Fatal(err)
			}

			preview, err := extractPreview(body, pageURL)
			if err != nil {
				t.Logf("extraction error: %v", err)
				return
			}

			imageSource := "readability"
			fallbackRequired := preview.Image == ""
			fallbackCandidates := []string(nil)
			if fallbackRequired {
				imageSource = "fallback required"
				fallbackCandidates = fallbackImageURLs(string(body), pageURL)
			}

			t.Logf("title (readability): %q", preview.Title)
			t.Logf("description (readability): %q", preview.Description)
			t.Logf("image (%s): %q", imageSource, preview.Image)
			t.Logf("favicon: %q", preview.Favicon)
			t.Logf("fallback required: %t", fallbackRequired)
			if fallbackRequired {
				t.Logf("fallback candidates: %#v", fallbackCandidates)
			}
		})
	}
}
