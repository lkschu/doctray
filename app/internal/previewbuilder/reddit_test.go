package previewbuilder

import "testing"

func TestEnrichRedditOverridesGenericTitle(t *testing.T) {
	extraction := previewExtraction{
		Preview:     URLPreview{Title: "Reddit"},
		TitleSource: "readability",
	}

	enrichReddit(&extraction, `<shreddit-title title="A &amp; B">`)

	if got, want := extraction.Preview.Title, "A & B"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
	if got, want := extraction.TitleSource, "reddit"; got != want {
		t.Fatalf("title source = %q, want %q", got, want)
	}
}
