package previewbuilder

import (
	"net"
	"net/url"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{address: "8.8.8.8", want: true},
		{address: "127.0.0.1", want: false},
		{address: "10.0.0.1", want: false},
		{address: "169.254.169.254", want: false},
		{address: "100.64.0.1", want: false},
		{address: "::1", want: false},
		{address: "fc00::1", want: false},
	}

	for _, test := range tests {
		if got := isPublicIP(net.ParseIP(test.address)); got != test.want {
			t.Errorf("isPublicIP(%q) = %t, want %t", test.address, got, test.want)
		}
	}
}

func TestResolvePreviewURL(t *testing.T) {
	baseURL, err := url.Parse("https://example.com/articles/one")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		rawURL string
		want   string
	}{
		{rawURL: "/image.jpg", want: "https://example.com/image.jpg"},
		{rawURL: "//cdn.example.com/image.jpg", want: "https://cdn.example.com/image.jpg"},
		{rawURL: "https://images.example.com/image.jpg", want: "https://images.example.com/image.jpg"},
		{rawURL: "http://127.0.0.1/image.jpg", want: ""},
	}

	for _, test := range tests {
		if got := resolvePreviewURL(baseURL, test.rawURL); got != test.want {
			t.Errorf("resolvePreviewURL(%q) = %q, want %q", test.rawURL, got, test.want)
		}
	}
}
