package previewbuilder

import "testing"

// TestSavedPreviewFixtures is a deliberately disabled offline fixture suite.
//
// Save representative pages as testdata/<name>.html rather than fetching them
// during tests. Once preview extraction is separated from network fetching,
// this test should feed each fixture into that extraction function and compare
// its title, description, and image with the expected values below.
func TestSavedPreviewFixtures(t *testing.T) {
	t.Skip("add saved HTML fixtures and an extraction-only test seam")

	/*
		fixtures := []struct {
			name            string
			sourceURL       string
			wantTitle       string
			wantDescription string
			wantImage       string
		}{
			{
				name:            "reddit-post",
				sourceURL:       "https://www.reddit.com/r/example/comments/example/",
				wantTitle:       "Expected title",
				wantDescription: "Expected description",
				wantImage:       "https://i.redd.it/example.jpg",
			},
		}

		for _, fixture := range fixtures {
			t.Run(fixture.name, func(t *testing.T) {
				// Read testdata/<fixture.name>.html.
				// Call an extraction-only helper with fixture.sourceURL and the HTML.
				// Compare its fields with fixture.wantTitle, fixture.wantDescription,
				// and fixture.wantImage.
			})
		}
	*/
}
