package main

import (
	"bytes"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"mime/multipart"
	"os"
	"path/filepath"
	"reflect"
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
	tmpl, err := template.ParseFiles("templates/base/composer.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := tmpl.ExecuteTemplate(&rendered, "base/composer.tmpl", profile_data{}); err != nil {
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
	for _, id := range []string{"form", "docUpload-text", "docUpload", "docUpload-label", "docUpload-attachments", "docUpload-attachment-summary", "docUpload-error", "upload-button"} {
		if elements[id] == nil {
			t.Fatalf("composer element %q missing", id)
		}
	}
	form := elements["form"]
	if attribute(form, "hx-post") != "/tray/doc-create" || attribute(form, "hx-encoding") != "multipart/form-data" || attribute(form, "hx-target") != "#doc-container" {
		t.Error("composer does not use the single multipart HTMX submission path")
	}
	if attribute(form, "hx-sync") != "#tray-container:queue all" {
		t.Error("sends must serialize with workspace replacements")
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
	sendDisabled := false
	for _, attr := range elements["upload-button"].Attr {
		if attr.Key == "disabled" {
			sendDisabled = true
		}
	}
	if !sendDisabled {
		t.Error("Send must start disabled for an empty draft")
	}
	if attribute(form, "aria-busy") != "false" {
		t.Error("composer must start idle")
	}
	summary := elements["docUpload-attachment-summary"]
	if attribute(summary, "role") != "status" || attribute(summary, "aria-live") != "polite" {
		t.Error("attachment count and size must be announced politely")
	}
	summaryHidden := false
	for _, attr := range summary.Attr {
		if attr.Key == "hidden" {
			summaryHidden = true
		}
	}
	if !summaryHidden {
		t.Error("attachment summary must start hidden for an empty draft")
	}
}

func TestTrayLayoutTemplate(t *testing.T) {
	tmpl, err := template.ParseGlob("templates/*/*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	for _, tagEdit := range []bool{false, true} {
		profile := profile_data{Tag_edit: tagEdit, Tags: []tag{{ID: "test-tag", Nr: "0", Name: "Test"}}}
		var rendered bytes.Buffer
		if err := tmpl.ExecuteTemplate(&rendered, "posts/tray.tmpl", profile); err != nil {
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
		composerCount := 0
		var visit func(*htmlparser.Node)
		visit = func(node *htmlparser.Node) {
			if node.Type == htmlparser.ElementNode {
				id := attribute(node, "id")
				if id != "" {
					elements[id] = node
				}
				if id == "uploadform" {
					composerCount++
				}
				if attribute(node, "hx-post") == "/tray/tag-apply" {
					if attribute(node, "hx-target") != "#workspace-container" || attribute(node, "hx-swap") != "outerHTML scroll:#doc-container:bottom" {
						t.Error("applying tag edits must update only the workspace and scroll the message list")
					}
				}
				if attribute(node, "hx-target") == "#workspace-container" && attribute(node, "hx-sync") != "#tray-container:drop" {
					t.Error("mutable workspace controls must not enter the send queue")
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
		}
		visit(document)
		for _, id := range []string{"tray-container", "workspace-container", "doc-container", "uploadform"} {
			if elements[id] == nil {
				t.Fatalf("tray element %q missing (tag edit: %t)", id, tagEdit)
			}
		}
		if composerCount != 1 {
			t.Errorf("tray contains %d composers, want one", composerCount)
		}
		if elements["uploadform"].Parent != elements["tray-container"] || elements["workspace-container"].Parent != elements["tray-container"] {
			t.Error("composer must be a sibling of the replaceable workspace")
		}
		for _, fragment := range []string{"posts/workspace-container.tmpl", "base/doc-list.tmpl"} {
			rendered.Reset()
			if err := tmpl.ExecuteTemplate(&rendered, fragment, profile); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rendered.String(), `id="uploadform"`) || strings.Contains(rendered.String(), `id="docUpload-text"`) {
				t.Errorf("%s recreates the composer and would lose its draft", fragment)
			}
		}
	}
}

func TestTagFilterTemplate(t *testing.T) {
	tmpl, err := template.ParseFiles("templates/base/tags.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tags := []tag{
		{ID: "reading-id", Nr: "0", Name: "Reading & notes", Sym: "📚", Color: "#335599", Enabled: true},
		{ID: "work-id", Nr: "1", Name: "Work", Color: "#ffffff"},
		{ID: "unnamed-id", Nr: "2", Color: "#335599"},
		{ID: "whitespace-id", Nr: "3", Name: " \t ", Color: "#335599"},
	}
	inactiveTags := append([]tag(nil), tags...)
	for i := range inactiveTags {
		inactiveTags[i].Enabled = false
	}
	for _, test := range []struct {
		name    string
		profile profile_data
		starred string
		clear   bool
	}{
		{name: "tags", profile: profile_data{Tags: tags}, starred: "false", clear: true},
		{name: "starred", profile: profile_data{Tags: tags, Only_favorites: true}, starred: "true", clear: true},
		{name: "inactive tags", profile: profile_data{Tags: inactiveTags}, starred: "false"},
		{name: "starred without tags", profile: profile_data{Only_favorites: true}, starred: "true", clear: true},
		{name: "no tags", starred: "false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var rendered bytes.Buffer
			if err := tmpl.ExecuteTemplate(&rendered, "base/tags.tmpl", test.profile); err != nil {
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
					if node.Data == "input" {
						t.Error("browsing filters must not contain editor inputs or hidden checkboxes")
					}
					if class := attribute(node, "class"); class == "tag-filter-heading" || class == "tag-filter-color" {
						t.Error("filter bar must not retain the heading or colour dots")
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					visit(child)
				}
			}
			visit(document)
			if star := elements["star-filter-button"]; star == nil || attribute(star, "class") != "tag-star-filter" {
				t.Error("Starred only must be a separate utility control, not a tag chip")
			}
			if !strings.Contains(rendered.String(), "<span>Starred only</span>") {
				t.Error("starred filter must have a visible Starred only label")
			}
			wantPressed := map[string]string{"star-filter-button": test.starred}
			for _, tag := range test.profile.Tags {
				pressed := "false"
				if tag.Enabled {
					pressed = "true"
				}
				wantPressed["select-tag-"+tag.Nr] = pressed
			}
			for id, pressed := range wantPressed {
				node := elements[id]
				if node == nil {
					t.Fatalf("filter %q missing", id)
				}
				if node.Data != "button" || attribute(node, "type") != "button" || attribute(node, "aria-pressed") != pressed {
					t.Errorf("%s must be a non-submit toggle with aria-pressed=%s", id, pressed)
				}
				if attribute(node, "hx-target") != "#workspace-container" || attribute(node, "hx-sync") != "#tray-container:drop" {
					t.Errorf("%s must retain workspace-only, send-safe updates", id)
				}
			}
			clear := elements["clear-filters-button"]
			if (clear != nil) != test.clear {
				t.Errorf("Clear filters visible = %t, want %t", clear != nil, test.clear)
			}
			if clear != nil {
				if clear.Data != "button" || attribute(clear, "type") != "button" || attribute(clear, "hx-post") != "/tray/filters-clear" {
					t.Error("Clear filters must use its single reset endpoint without submitting a form")
				}
				if attribute(clear, "hx-target") != "#workspace-container" || attribute(clear, "hx-sync") != "#tray-container:drop" || attribute(clear, "hx-swap") != "outerHTML scroll:#doc-container:bottom" {
					t.Error("Clear filters must retain workspace-only, send-safe updates")
				}
			}
			if len(test.profile.Tags) > 0 {
				if !strings.Contains(attribute(elements["select-tag-0"], "style"), "--tag-color: #335599") || !strings.Contains(attribute(elements["select-tag-1"], "style"), "--tag-color: #ffffff") {
					t.Error("each chip must supply its own tag colour for its outline and selected background")
				}
				if attribute(elements["select-tag-0"], "hx-post") != "/tray/tag-toggle-filter" || attribute(elements["select-tag-0"], "hx-vals") != `{"id":"reading-id"}` {
					t.Error("tag chip must toggle by stable tag ID through the existing endpoint")
				}
				if !strings.Contains(rendered.String(), "Reading &amp; notes") {
					t.Error("tag names must remain HTML-escaped")
				}
				for _, id := range []string{"select-tag-2", "select-tag-3"} {
					if attribute(elements[id], "title") != "Unnamed tag" {
						t.Error("empty or whitespace-only tag names must have a readable fallback")
					}
				}
			}
			manage := elements["tags-edit-button"]
			if manage == nil {
				t.Fatal("Manage tags must remain available, including when there are no tags")
			}
			if attribute(manage, "hx-post") != "/tray/tag-edit" || attribute(manage, "hx-target") != "#tag-container" {
				t.Error("Manage tags must open the existing editor without replacing the composer")
			}
		})
	}
}

func TestClearFilters(t *testing.T) {
	if (profile_data{}).HasActiveFilters() {
		t.Error("empty profiles must have no active filters")
	}
	for _, test := range []struct {
		name    string
		tag     bool
		starred bool
	}{
		{name: "inactive"},
		{name: "tags only", tag: true},
		{name: "starred only", starred: true},
		{name: "both", tag: true, starred: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := profile_data{
				Only_favorites: test.starred,
				Tag_edit:       true,
				Tags: []tag{
					{ID: "reading", Nr: "0", Name: "Reading", Sym: "📚", Color: "#335599", Enabled: test.tag},
					{ID: "work", Nr: "1", Name: "Work", Color: "#ffffff", Enabled: test.tag},
				},
				Posts: []post{{DocID: 1, Title: "Keep this message", Starred: true, Tags: []string{"reading"}}},
			}
			if profile.HasActiveFilters() != (test.tag || test.starred) {
				t.Error("active filter detection does not match tag/starred state")
			}
			want := profile
			want.Only_favorites = false
			want.Tags = append([]tag(nil), profile.Tags...)
			want.Posts = append([]post(nil), profile.Posts...)
			want.Posts[0].Tags = append([]string(nil), profile.Posts[0].Tags...)
			for i := range want.Tags {
				want.Tags[i].Enabled = false
			}
			for i := 0; i < 2; i++ {
				profile.ClearFilters()
				if profile.HasActiveFilters() || !reflect.DeepEqual(profile, want) {
					t.Error("clearing filters must be idempotent and preserve tag definitions, posts, and editor state")
				}
			}
		})
	}
}

func TestTagsFromMultiform(t *testing.T) {
	saved := []tag{{ID: "reading", Enabled: true}, {ID: "removed", Enabled: true}}
	form := &multipart.Form{Value: map[string][]string{
		"tag[0]tag_id": {"reading"},
		"tag[0]name":   {" Research & <notes>\r "},
		"tag[0]symbol": {"👩🏽‍💻"},
		"tag[0]color":  {"#335599"},
		// A gap represents a locally removed row; new tags start unselected.
		"tag[2]tag_id": {"new-tag"},
		"tag[2]name":   {"New tag"},
		"tag[2]symbol": {""},
		"tag[2]color":  {"#ffffff"},
	}}
	want := []tag{
		{ID: "reading", Nr: "0", Name: "Research & <notes>", Sym: "👩🏽‍💻", Color: "#335599", Enabled: true},
		{ID: "new-tag", Nr: "2", Name: "New tag", Color: "#ffffff"},
	}
	if got := tagsFromMultiform(form, saved); !reflect.DeepEqual(got, want) {
		t.Errorf("parsed tags = %#v, want %#v", got, want)
	}
	if !saved[0].Enabled || !saved[1].Enabled {
		t.Error("parsing a draft must not modify saved tags")
	}
	if got := tagsFromMultiform(&multipart.Form{Value: map[string][]string{}}, saved); len(got) != 0 {
		t.Error("removing all rows must produce an empty tag list")
	}
}

func TestTagEditorTemplate(t *testing.T) {
	tmpl, err := template.ParseFiles("templates/base/tags.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	tags := []tag{
		{ID: "reading", Nr: "0", Name: "Research & <notes>", Sym: "👩🏽‍💻", Color: "#335599", Enabled: true},
		{ID: "work", Nr: "1", Name: "Work", Color: "#ffffff"},
	}
	for _, draft := range [][]tag{tags, nil} {
		var rendered bytes.Buffer
		if err := tmpl.ExecuteTemplate(&rendered, "base/tags.tmpl", profile_data{Tag_edit: true, Tags: draft}); err != nil {
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
		form := &multipart.Form{Value: make(map[string][]string)}
		rows, removeButtons := 0, 0
		var visit func(*htmlparser.Node)
		visit = func(node *htmlparser.Node) {
			if node.Type == htmlparser.ElementNode {
				if id := attribute(node, "id"); id != "" {
					if elements[id] != nil {
						t.Errorf("duplicate editor element ID %q", id)
					}
					elements[id] = node
				}
				if node.Data == "input" {
					form.Value[attribute(node, "name")] = []string{attribute(node, "value")}
					if attribute(node, "type") != "hidden" && attribute(node, "aria-label") == "" {
						t.Error("editable inputs need accessible labels")
					}
					if attribute(node, "maxlength") != "" {
						t.Error("emoji inputs must not truncate multi-codepoint emoji")
					}
				}
				if attribute(node, "class") == "tag-editor-row" {
					rows++
				}
				if attribute(node, "class") == "tag-editor-remove" {
					removeButtons++
					if attribute(node, "type") != "button" || attribute(node, "hx-post") != "" || attribute(node, "aria-label") == "" {
						t.Error("Remove must be a labelled local action, not a request or form submission")
					}
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
		}
		visit(document)
		for _, id := range []string{"tag-editor-form", "tag-add-button", "tag-save-button", "tag-cancel-button"} {
			if elements[id] == nil {
				t.Fatalf("editor element %q missing", id)
			}
		}
		editor := elements["tag-editor-form"]
		if attribute(editor, "hx-post") != "/tray/tag-apply" || attribute(editor, "hx-encoding") != "multipart/form-data" || attribute(editor, "hx-target") != "#workspace-container" || attribute(editor, "hx-sync") != "#tray-container:drop" {
			t.Error("Save must submit one send-safe multipart request and update only the workspace")
		}
		if attribute(elements["tag-save-button"], "type") != "submit" {
			t.Error("Save changes must submit the form, including keyboard submission")
		}
		for id, endpoint := range map[string]string{"tag-add-button": "/tray/tag-create", "tag-cancel-button": "/tray/tag-cancel"} {
			node := elements[id]
			if attribute(node, "type") != "button" || attribute(node, "hx-post") != endpoint || attribute(node, "hx-target") != "#tag-container" || attribute(node, "hx-sync") != "#tray-container:drop" {
				t.Errorf("%s must use its editor-only, send-safe action without submitting Save", id)
			}
		}
		if attribute(elements["tag-cancel-button"], "hx-params") != "none" {
			t.Error("Cancel must not send unapplied editor fields")
		}
		if rows != len(draft) || removeButtons != len(draft) {
			t.Error("each tag must have one row and one removal control")
		}
		if parsed := tagsFromMultiform(form, tags); len(draft) > 0 && !reflect.DeepEqual(parsed, draft) {
			t.Error("editor round-trips must preserve names, emoji, IDs and filter selections without repeated escaping")
		}
		if len(draft) == 0 && !strings.Contains(rendered.String(), "No tags yet") {
			t.Error("empty editors must explain how to add a tag")
		}
	}
}
