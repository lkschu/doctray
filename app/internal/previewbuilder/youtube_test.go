package previewbuilder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestYouTubeVideoURL(t *testing.T) {
	for _, test := range []struct {
		url  string
		want string
	}{
		{url: "https://youtube.com/watch?v=AZmql5nbTl0", want: "AZmql5nbTl0"},
		{url: "https://www.youtube.com/watch?v=AZmql5nbTl0&t=120&list=playlist#section", want: "AZmql5nbTl0"},
		{url: "https://m.youtube.com/watch?v=AZmql5nbTl0", want: "AZmql5nbTl0"},
		{url: "https://music.youtube.com/watch?v=AZmql5nbTl0", want: "AZmql5nbTl0"},
		{url: "https://youtu.be/AZmql5nbTl0?si=tracking&t=12", want: "AZmql5nbTl0"},
		{url: "https://www.youtu.be/AZmql5nbTl0", want: "AZmql5nbTl0"},
		{url: "https://www.youtube.com/shorts/QGcIMmgB6_8", want: "QGcIMmgB6_8"},
		{url: "https://www.youtube.com/embed/AZmql5nbTl0", want: "AZmql5nbTl0"},
		{url: "https://www.youtube.com/live/AZmql5nbTl0", want: "AZmql5nbTl0"},
		{url: "http://WWW.YOUTUBE.COM/watch?v=AZmql5nbTl0", want: "AZmql5nbTl0"},
		{url: "https://www.youtube.com/watch?v=aB_-0123456", want: "aB_-0123456"},
		{url: "https://www.youtube.com/"},
		{url: "https://www.youtube.com/@channel"},
		{url: "https://www.youtube.com/playlist?list=playlist"},
		{url: "https://www.youtube.com/watch"},
		{url: "https://www.youtube.com/watch?v=too-short"},
		{url: "https://www.youtube.com/watch?v=AZmql5nbTl00"},
		{url: "https://www.youtube.com/watch?v=AZmql5nbTl!"},
		{url: "https://youtu.be/AZmql5nbTl0/extra"},
		{url: "https://www.youtube.com/shorts/"},
		{url: "https://example.com/watch?v=AZmql5nbTl0"},
		{url: "https://youtube.com.example.com/watch?v=AZmql5nbTl0"},
		{url: "https://notyoutube.com/watch?v=AZmql5nbTl0"},
		{url: "https://www.youtube.com@evil.example/watch?v=AZmql5nbTl0"},
		{url: "https://user:secret@www.youtube.com/watch?v=AZmql5nbTl0"},
		{url: "ftp://www.youtube.com/watch?v=AZmql5nbTl0"},
		{url: "https://www.youtube.com/%zz"},
	} {
		t.Run(test.url, func(t *testing.T) {
			pageURL, id, handled := youtubeVideoURL(test.url)
			if handled != (test.want != "") || id != test.want {
				t.Fatalf("video ID = %q, handled = %t; want %q", id, handled, test.want)
			}
			if handled && pageURL.String() != test.url {
				t.Error("recognition must preserve the original link")
			}
		})
	}
}

type youtubeTestTransport func(*http.Request) (*http.Response, error)

func (transport youtubeTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func stubYouTubePreviewRequests(t *testing.T, transport youtubeTestTransport) {
	t.Helper()
	previous := previewHTTPClient
	client := *previous
	client.Transport = transport
	previewHTTPClient = &client
	t.Cleanup(func() { previewHTTPClient = previous })
}

func youtubeTestResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    request,
	}
}

func TestBuildYouTubePreview(t *testing.T) {
	body, err := os.ReadFile("testdata/youtube-watch.oembed.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, inputURL := range []string{
		"https://youtube.com/watch?v=AZmql5nbTl0",
		"https://www.youtube.com/watch?v=AZmql5nbTl0&t=42&list=playlist",
		"https://m.youtube.com/watch?v=AZmql5nbTl0",
		"https://youtu.be/AZmql5nbTl0?si=private-share-token&t=42",
		"https://www.youtube.com/shorts/AZmql5nbTl0",
	} {
		t.Run(inputURL, func(t *testing.T) {
			calls := 0
			stubYouTubePreviewRequests(t, func(request *http.Request) (*http.Response, error) {
				calls++
				query := request.URL.Query()
				if request.Method != http.MethodGet || request.URL.Scheme != "https" || request.URL.Host != "www.youtube.com" || request.URL.Path != "/oembed" || len(query) != 2 || query.Get("format") != "json" || query.Get("url") != "https://www.youtube.com/watch?v=AZmql5nbTl0" {
					t.Fatalf("unexpected request: %s", request.URL)
				}
				return youtubeTestResponse(request, http.StatusOK, string(body)), nil
			})
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			preview, err := BuildURLPreview(context.Background(), logger, inputURL, "")
			if err != nil || calls != 1 {
				t.Fatalf("YouTube enrichment must succeed without fetching the watch page: calls %d, error %v", calls, err)
			}
			if preview.URL != inputURL || preview.Domain != previewHost(inputURL) || preview.Title != "Why everyone smells the same now." || preview.Description != "By The  Perfumed Perspective" || preview.Image != "https://i.ytimg.com/vi/AZmql5nbTl0/hqdefault.jpg" || preview.Favicon != "https://www.youtube.com/favicon.ico" || preview.Image == preview.Favicon || preview.Pending {
				t.Fatal("video previews must retain the original link and use the real title, channel and thumbnail")
			}
			if !strings.Contains(logs.String(), `"source":"youtube_oembed"`) {
				t.Error("resolved previews must identify the YouTube source")
			}
			for _, private := range []string{inputURL, "AZmql5nbTl0", preview.Title, preview.Description, preview.Image, "private-share-token"} {
				if strings.Contains(logs.String(), private) {
					t.Error("even DEBUG logs must not contain video links, metadata or share tokens")
				}
			}
		})
	}
}

func TestYouTubeOEmbedFailureKeepsHTMLPreview(t *testing.T) {
	shortsHTML, err := os.ReadFile("testdata/youtube-shorts.html")
	if err != nil {
		t.Fatal(err)
	}
	const validFields = `"type":"video","title":"Video","thumbnail_url":"https://i.ytimg.com/vi/QGcIMmgB6_8/hqdefault.jpg"`
	for _, test := range []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{name: "not available", status: http.StatusNotFound},
		{name: "provider failure", status: http.StatusServiceUnavailable},
		{name: "network error", err: errors.New("private video request failure")},
		{name: "invalid JSON", status: http.StatusOK, body: "not JSON"},
		{name: "missing title", status: http.StatusOK, body: `{"type":"video","thumbnail_url":"https://i.ytimg.com/image.jpg"}`},
		{name: "blank title", status: http.StatusOK, body: `{"type":"video","title":" \u0000 ","thumbnail_url":"https://i.ytimg.com/image.jpg"}`},
		{name: "wrong type", status: http.StatusOK, body: `{"type":"rich","title":"Video","thumbnail_url":"https://i.ytimg.com/image.jpg"}`},
		{name: "missing thumbnail", status: http.StatusOK, body: `{"type":"video","title":"Video"}`},
		{name: "private thumbnail", status: http.StatusOK, body: `{"type":"video","title":"Video","thumbnail_url":"http://127.0.0.1/private.jpg"}`},
		{name: "unsafe thumbnail", status: http.StatusOK, body: `{"type":"video","title":"Video","thumbnail_url":"javascript:alert(1)"}`},
		{name: "oversized response", status: http.StatusOK, body: "{" + validFields + `,"extra":"` + strings.Repeat("x", maxPreviewBodyBytes) + `"}`},
		{name: "trailing JSON", status: http.StatusOK, body: "{" + validFields + "} {}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			stubYouTubePreviewRequests(t, func(request *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 && request.URL.Host == "www.youtube.com" && request.URL.Path == "/oembed" {
					if request.URL.Query().Get("url") != "https://www.youtube.com/watch?v=QGcIMmgB6_8" {
						t.Error("Shorts lookup must use its canonical watch URL")
					}
					if test.err != nil {
						return nil, test.err
					}
					return youtubeTestResponse(request, test.status, test.body), nil
				}
				if calls == 2 && request.URL.Host == "www.youtube.com" && request.URL.Path == "/shorts/QGcIMmgB6_8" {
					return youtubeTestResponse(request, http.StatusOK, string(shortsHTML)), nil
				}
				t.Fatalf("unexpected fallback request: %s", request.URL)
				return nil, errors.New("unexpected request")
			})
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			preview, err := BuildURLPreview(context.Background(), logger, "https://www.youtube.com/shorts/QGcIMmgB6_8", "")
			if err != nil || calls != 2 || preview.Title != "Make Your Dog Wait At Doors!" || !strings.HasPrefix(preview.Image, "https://i.ytimg.com/vi/QGcIMmgB6_8/") {
				t.Fatal("oEmbed failure must preserve the existing rich Shorts HTML preview")
			}
			if !strings.Contains(logs.String(), `"reason":"youtube_oembed_error"`) || strings.Contains(logs.String(), "QGcIMmgB6_8") || strings.Contains(logs.String(), "private video request failure") {
				t.Error("fallback diagnostics must identify the source failure without private request details")
			}
		})
	}
}

func TestOtherPagesSkipYouTubeOEmbed(t *testing.T) {
	stubYouTubePreviewRequests(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://example.com/page" {
			t.Fatalf("ordinary links must not invoke a YouTube source request: %s", request.URL)
		}
		return youtubeTestResponse(request, http.StatusOK, `<html><head><title>Ordinary page</title><meta property="og:image" content="https://example.com/image.jpg"></head><body><p>`+strings.Repeat("Ordinary article text. ", 50)+`</p></body></html>`), nil
	})
	preview, err := BuildURLPreview(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)), "https://example.com/page", "")
	if err != nil || preview.Title != "Ordinary page" {
		t.Fatal("non-video links must retain generic page extraction")
	}
}
