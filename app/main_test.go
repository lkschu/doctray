package main

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"doctray/internal/previewbuilder"
	"doctray/internal/thumbnail"
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

func TestRemoveAttachment(t *testing.T) {
	directory := t.TempDir()
	original := filepath.Join(directory, "upload.png")
	for _, filename := range []string{original, thumbnail.Path(original)} {
		if err := os.WriteFile(filename, []byte("contents"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	attachment := docentry_file{Url: "/media/upload.png"}
	// Also handles old records with no stored thumbnail URL.
	removeAttachment(directory, attachment, logger)
	removeAttachment(directory, attachment, logger)
	for _, filename := range []string{original, thumbnail.Path(original)} {
		if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("file still exists after deletion: %v", err)
		}
	}
}

func TestAttachmentTemplateOnlyLoadsThumbnail(t *testing.T) {
	tmpl, err := template.ParseFiles("templates/base/doc.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	for _, thumbnailURL := range []string{"", "/media/photo.jpg.thumb.png"} {
		var rendered bytes.Buffer
		attachment := docentry_file{Url: "/media/photo.jpg", ThumbnailURL: thumbnailURL, Icon: "imagesmode"}
		if err := tmpl.ExecuteTemplate(&rendered, "base/doc-url.tmpl", []docentry_file{attachment}); err != nil {
			t.Fatal(err)
		}
		output := rendered.String()
		if strings.Contains(output, `src="/media/photo.jpg"`) {
			t.Fatal("template loads the original as a preview")
		}
		if !strings.Contains(output, `href="/media/photo.jpg"`) {
			t.Error("original attachment link missing")
		}
		if thumbnailURL == "" && strings.Contains(output, "<img") {
			t.Error("template renders an image without a thumbnail")
		}
		if thumbnailURL != "" && !strings.Contains(output, `src="`+thumbnailURL+`"`) {
			t.Error("thumbnail image missing")
		}
	}
}
