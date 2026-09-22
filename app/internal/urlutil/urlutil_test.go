package urlutil

import "testing"

func TestNormalizeHTTPURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "adds HTTPS to a Reddit URL",
			input: "reddit.com/r/kde/",
			want:  "https://reddit.com/r/kde/",
		},
		{
			name:  "preserves HTTPS",
			input: "https://www.reddit.com/r/kde/",
			want:  "https://www.reddit.com/r/kde/",
		},
		{
			name:  "preserves HTTP",
			input: "http://localhost:3000/test",
			want:  "http://localhost:3000/test",
		},
		{
			name:  "unescapes an HTML encoded query",
			input: "reddit.com/r/kde/?sort=hot&amp;limit=10",
			want:  "https://reddit.com/r/kde/?sort=hot&limit=10",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeHTTPURL(test.input)
			if err != nil {
				t.Fatalf("NormalizeHTTPURL(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Errorf("NormalizeHTTPURL(%q) = %q, want %q",
					test.input, got, test.want)
			}
		})
	}
}
