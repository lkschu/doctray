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

	htmlparser "golang.org/x/net/html"
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

func TestComposerTemplate(t *testing.T) {
	tmpl, err := template.ParseFiles("templates/base/doc-list.tmpl", "templates/base/doc.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := tmpl.ExecuteTemplate(&rendered, "base/doc-list.tmpl", profile_data{}); err != nil {
		t.Fatal(err)
	}
	document, err := htmlparser.Parse(&rendered)
	if err != nil {
		t.Fatal(err)
	}
	attribute := func(node *htmlparser.Node, name string) string {
		for _, attr := range node.Attr {
			if attr.Key == name {
				return attr.Val
			}
		}
		return ""
	}
	elements := make(map[string]*htmlparser.Node)
	var visit func(*htmlparser.Node)
	visit = func(node *htmlparser.Node) {
		if node.Type == htmlparser.ElementNode {
			if id := attribute(node, "id"); id != "" {
				elements[id] = node
			}
			for _, attr := range node.Attr {
				if strings.HasPrefix(attr.Key, "on") {
					t.Errorf("composer retains an inline event handler: %s", attr.Key)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	for _, id := range []string{"form", "docUpload-text", "docUpload", "docUpload-label", "docUpload-attachments", "docUpload-error", "upload-button"} {
		if elements[id] == nil {
			t.Fatalf("composer element %q missing", id)
		}
	}
	form := elements["form"]
	if attribute(form, "hx-post") != "/tray/doc-create" || attribute(form, "hx-encoding") != "multipart/form-data" || attribute(form, "hx-target") != "#doc-container" {
		t.Error("composer does not use the single multipart HTMX submission path")
	}
	for _, id := range []string{"docUpload-text", "docUpload"} {
		for _, attr := range elements[id].Attr {
			if attr.Key == "required" {
				t.Errorf("%s is required, preventing text-only or file-only drafts", id)
			}
		}
	}
	if attribute(elements["docUpload-label"], "type") != "button" {
		t.Error("attachment picker button can submit the draft")
	}
	if elements["docUpload-label"].Parent != elements["upload-button"].Parent || attribute(elements["docUpload-label"].Parent, "class") != "composer-actions" {
		t.Error("attachment button must share the side action column with Send")
	}
	if attribute(elements["docUpload-label"], "aria-label") != "Attach files" {
		t.Error("icon-only attachment button needs an accessible label")
	}
	if elements["docUpload-hint"] != nil || attribute(elements["docUpload-text"], "aria-describedby") != "" {
		t.Error("composer retains the removed keyboard hint")
	}
	if attribute(elements["upload-button"], "type") != "submit" || attribute(elements["upload-button"], "hx-post") != "" {
		t.Error("Send must submit the form rather than use a separate request path")
	}
}
