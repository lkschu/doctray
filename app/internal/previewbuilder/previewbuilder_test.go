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

func TestFallbackImageURLs(t *testing.T) {
	baseURL, err := url.Parse("https://example.com/articles/one")
	if err != nil {
		t.Fatal(err)
	}

	imageURLs := fallbackImageURLs(`
		<img src="/images/one.jpg">
		<img src="//cdn.example.com/two.jpg">
		<img src="/images/one.jpg">
		<img src="http://127.0.0.1/private.jpg">
	`, baseURL)
	want := []string{
		"https://example.com/images/one.jpg",
		"https://cdn.example.com/two.jpg",
	}

	if len(imageURLs) != len(want) {
		t.Fatalf("got %d image URLs, want %d", len(imageURLs), len(want))
	}
	for i := range want {
		if imageURLs[i] != want[i] {
			t.Errorf("imageURLs[%d] = %q, want %q", i, imageURLs[i], want[i])
		}
	}
}

func TestHostFallbackPreview(t *testing.T) {
	pageURL, err := url.Parse("https://www.example.com/articles/one?source=doctray#section")
	if err != nil {
		t.Fatal(err)
	}

	preview := hostFallbackPreview(pageURL)
	if got, want := preview.Title, "www.example.com"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got, want := preview.Image, "https://www.example.com/favicon.ico"; got != want {
		t.Errorf("image = %q, want %q", got, want)
	}
	if got, want := preview.ImageFallback, "https://www.example.com/favicon"; got != want {
		t.Errorf("image fallback = %q, want %q", got, want)
	}
	if preview.Image != preview.Favicon {
		t.Errorf("image = %q, favicon = %q, want matching values", preview.Image, preview.Favicon)
	}
}

func TestURLFallbackPreview(t *testing.T) {
	preview, err := urlFallbackPreview("https://www.example.com/articles/one?source=doctray")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := preview.Title, "www.example.com"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got, want := preview.Image, previewPlaceholderImage; got != want {
		t.Errorf("image = %q, want %q", got, want)
	}
	if preview.ImageFallback != "" {
		t.Errorf("image fallback = %q, want empty", preview.ImageFallback)
	}
}

func TestURLFallbackPreviewRejectsInvalidURL(t *testing.T) {
	if _, err := urlFallbackPreview("mailto:user@example.com"); err == nil {
		t.Fatal("urlFallbackPreview accepted a non-HTTP URL")
	}
}

func TestIsChallengePreview(t *testing.T) {
	tests := []struct {
		name    string
		preview URLPreview
		body    string
		want    bool
	}{
		{name: "cloudflare title", preview: URLPreview{Title: "Just a moment..."}, want: true},
		{name: "cloudflare marker", preview: URLPreview{Title: "Example"}, body: `<script src="https://challenges.cloudflare.com/turnstile/v0/api.js"></script>`, want: true},
		{name: "reddit challenge", preview: URLPreview{Title: "Reddit"}, body: `<input name="js_challenge">`, want: true},
		{name: "ordinary page", preview: URLPreview{Title: "Example"}, body: `<title>Example</title>`, want: false},
	}

	for _, test := range tests {
		if got := isChallengePreview(test.preview, []byte(test.body)); got != test.want {
			t.Errorf("%s: isChallengePreview() = %t, want %t", test.name, got, test.want)
		}
	}
}
