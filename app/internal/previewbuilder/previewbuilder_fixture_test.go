package previewbuilder

import (
	"bytes"
	"net/url"
	"os"
	"testing"
)

// TestSavedPreviewFixtures runs saved source-handler responses or generic page
// extraction and enrichers. Set DOCTRAY_TMDB_LIVE_TEST=1 to use the configured
// TMDb key for the IMDb fixture. Image fallback remains diagnostic because
// selecting an image requires network I/O.
func TestSavedPreviewFixtures(t *testing.T) {
	fixtures := []struct {
		name      string
		filename  string
		sourceURL string
		oEmbed    string
		liveTMDb  bool
	}{
		{
			name:      "imdb-title",
			filename:  "imdb.html",
			sourceURL: "https://www.imdb.com/de/title/tt0107007/",
			liveTMDb:  true,
		},
		{
			name:      "reddit-overlay-mod",
			filename:  "reddit-overlay_mod.html",
			sourceURL: "https://www.reddit.com/r/MonsterHunterWorld/comments/17xmkrf/overlay_mod",
			oEmbed:    "reddit-overlay_mod.oembed.json",
		},
		{
			name:      "youtube-watch",
			filename:  "youtube-watch.html",
			sourceURL: "https://www.youtube.com/watch?v=AZmql5nbTl0",
		},
		{
			name:      "youtube-shorts",
			filename:  "youtube-shorts.html",
			sourceURL: "https://www.youtube.com/shorts/QGcIMmgB6_8",
		},
	}
	liveTMDb := os.Getenv("DOCTRAY_TMDB_LIVE_TEST") == "1"
	tmdbAPIKey := os.Getenv("DOCTRAY_TMDB_API_KEY")

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

			extraction := previewExtraction{}
			if fixture.liveTMDb && liveTMDb {
				if tmdbAPIKey == "" {
					t.Fatal("DOCTRAY_TMDB_API_KEY is required when DOCTRAY_TMDB_LIVE_TEST=1")
				}
				preview, handled, err := tmdbPreviewForIMDbTitle(fixture.sourceURL, tmdbAPIKey)
				if err != nil {
					t.Fatalf("TMDb lookup failed: %v", err)
				}
				if !handled {
					t.Fatal("TMDb lookup returned no result")
				}
				if preview.Title == "" {
					t.Fatal("TMDb lookup returned an empty title")
				}
				extraction = previewExtraction{
					Preview:           preview,
					TitleSource:       "TMDb",
					DescriptionSource: "TMDb",
					ImageSource:       "TMDb",
				}
				t.Log("TMDb lookup succeeded")
			} else if fixture.oEmbed != "" {
				oEmbed, err := os.ReadFile("testdata/" + fixture.oEmbed)
				if err != nil {
					t.Fatal(err)
				}
				preview, handled := redditOEmbedPreviewFromReader(pageURL, bytes.NewReader(oEmbed))
				if !handled {
					t.Fatal("saved oEmbed response did not produce a preview")
				}
				extraction = previewExtraction{
					Preview:           preview,
					TitleSource:       "reddit oEmbed",
					DescriptionSource: "reddit URL",
					ImageSource:       "reddit oEmbed favicon",
				}
			} else {
				extraction, err = extractPreview(body, pageURL)
				if err != nil {
					t.Logf("extraction error: %v", err)
					return
				}
			}

			imageSource := extraction.ImageSource
			fallbackRequired := extraction.Preview.Image == ""
			fallbackCandidates := []string(nil)
			if fallbackRequired {
				imageSource = "fallback required"
				fallbackCandidates = fallbackImageURLs(string(body), pageURL)
			}

			t.Logf("title (%s): %q", extraction.TitleSource, extraction.Preview.Title)
			t.Logf("description (%s): %q", extraction.DescriptionSource, extraction.Preview.Description)
			t.Logf("image (%s): %q", imageSource, extraction.Preview.Image)
			t.Logf("favicon: %q", extraction.Preview.Favicon)
			t.Logf("fallback required: %t", fallbackRequired)
			if fallbackRequired {
				t.Logf("fallback candidates: %#v", fallbackCandidates)
			}
		})
	}
}
