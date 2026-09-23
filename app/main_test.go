package main

import (
	"testing"

	"main/internal/previewbuilder"
)

func TestPostHasPendingPreviews(t *testing.T) {
	tests := []struct {
		name string
		post post
		want bool
	}{
		{name: "no previews", want: false},
		{
			name: "resolved preview",
			post: post{Webpreview: []previewbuilder.URLPreview{{Pending: false}}},
			want: false,
		},
		{
			name: "pending preview",
			post: post{Webpreview: []previewbuilder.URLPreview{{Pending: true}}},
			want: true,
		},
	}

	for _, test := range tests {
		if got := test.post.HasPendingPreviews(); got != test.want {
			t.Errorf("%s: HasPendingPreviews() = %t, want %t", test.name, got, test.want)
		}
	}
}
