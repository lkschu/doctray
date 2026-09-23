package previewbuilder

import "testing"

func TestRedditPostURL(t *testing.T) {
	tests := []struct {
		inputURL      string
		wantPost      bool
		wantSubreddit string
	}{
		{
			inputURL:      "https://www.reddit.com/r/MonsterHunterWorld/comments/17xmkrf/overlay_mod/",
			wantPost:      true,
			wantSubreddit: "r/MonsterHunterWorld",
		},
		{inputURL: "https://www.reddit.com/r/MonsterHunterWorld/", wantPost: false},
		{inputURL: "https://example.com/r/MonsterHunterWorld/comments/17xmkrf/overlay_mod/", wantPost: false},
		{inputURL: "https://www.reddit.com/user/example/comments/17xmkrf/overlay_mod/", wantPost: true},
	}

	for _, test := range tests {
		postURL, gotPost := redditPostURL(test.inputURL)
		if gotPost != test.wantPost {
			t.Errorf("redditPostURL(%q) = %t, want %t", test.inputURL, gotPost, test.wantPost)
			continue
		}
		if !gotPost {
			continue
		}
		if gotSubreddit := redditSubreddit(postURL); gotSubreddit != test.wantSubreddit {
			t.Errorf("redditSubreddit(%q) = %q, want %q", test.inputURL, gotSubreddit, test.wantSubreddit)
		}
	}
}
