package previewbuilder

import "testing"

func TestIMDbTitleID(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{url: "https://www.imdb.com/title/tt0107007/", want: "tt0107007"},
		{url: "https://www.imdb.com/de/title/tt0107007/?ref_=fn_all_ttl_1", want: "tt0107007"},
		{url: "https://m.imdb.com/title/tt0944947/", want: "tt0944947"},
		{url: "https://example.com/title/tt0107007/", want: ""},
		{url: "https://www.imdb.com/name/nm0000001/", want: ""},
		{url: "https://www.imdb.com/title/not-an-id/", want: ""},
	}

	for _, test := range tests {
		got, ok := imdbTitleID(test.url)
		if test.want == "" {
			if ok {
				t.Errorf("imdbTitleID(%q) matched %q, want no match", test.url, got)
			}
			continue
		}
		if !ok || got != test.want {
			t.Errorf("imdbTitleID(%q) = %q, %t; want %q, true", test.url, got, ok, test.want)
		}
	}
}
