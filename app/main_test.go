package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime/multipart"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
		{
			name: "deleted pending preview",
			post: post{DeletedAt: new(time.Time), Webpreview: []previewbuilder.URLPreview{{Pending: true}}},
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
	if !removeAttachment(directory, attachment, logger) || !removeAttachment(directory, attachment, logger) {
		t.Error("attachment removal must succeed even when files are already absent")
	}
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
		attachment := docentry_file{Url: "/media/photo.jpg", OrgName: "photo.jpg", ThumbnailURL: thumbnailURL, Icon: "imagesmode"}
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
		if !strings.Contains(output, `aria-label="Open photo.jpg"`) || !strings.Contains(output, `title="photo.jpg"`) {
			t.Error("attachment links must retain a complete filename when visual labels are truncated")
		}
		if thumbnailURL == "" && strings.Contains(output, "<img") {
			t.Error("template renders an image without a thumbnail")
		}
		if thumbnailURL != "" && !strings.Contains(output, `src="`+thumbnailURL+`"`) {
			t.Error("thumbnail image missing")
		}
	}
}

func TestPostDateFormatting(t *testing.T) {
	// The reference is in 2025 locally but 2026 in UTC.
	now := time.Date(2025, 12, 31, 23, 30, 0, 0, time.FixedZone("west", -3600))
	for _, test := range []struct {
		name     string
		date     string
		compact  string
		datetime string
	}{
		{name: "current UTC year", date: "Fri, 02 Jan 2026 14:05:59 GMT", compact: "2 Jan · 14:05 UTC", datetime: "2026-01-02T14:05:59Z"},
		{name: "previous year", date: "Wed, 31 Dec 2025 23:59:59 GMT", compact: "31 Dec 2025 · 23:59 UTC", datetime: "2025-12-31T23:59:59Z"},
		{name: "future year", date: "Sun, 03 Jan 2027 14:05:59 GMT", compact: "3 Jan 2027 · 14:05 UTC", datetime: "2027-01-03T14:05:59Z"},
		{name: "legacy", date: "unknown <date>", compact: "unknown <date>"},
		{name: "empty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := compactPostDate(test.date, now); got != test.compact {
				t.Errorf("compact date = %q, want %q", got, test.compact)
			}
			message := post{Date: test.date}
			if got := message.DateTime(); got != test.datetime {
				t.Errorf("datetime = %q, want %q", got, test.datetime)
			}
			if message.Date != test.date {
				t.Error("date presentation must not rewrite the stored value")
			}
		})
	}
}

func TestMessageCardTemplate(t *testing.T) {
	tmpl, err := template.ParseFiles("templates/base/doc.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	message := post{DocID: 42, Title: "Message text", Type: "msg", Starred: true, Date: "Tue, 03 Oct 2000 14:05:59 GMT",
		Webpreview: []previewbuilder.URLPreview{{ID: "pending", Pending: true, URL: "https://example.com/?a=1&b=2",
			Title: "Preview & <title>", Image: "/resources/preview-placeholder.svg", ImageFallback: "https://example.com/favicon"}},
		Files: []docentry_file{{Url: "/media/photo.jpg", OrgName: "Photo & <notes>.jpg", ThumbnailURL: "/media/photo.jpg.thumb.png"}}}
	for i := 0; i < 20; i++ {
		tag := tag{ID: fmt.Sprintf("tag-%d", i), Name: fmt.Sprintf("Tag %d", i), Sym: "📚", Color: "#335599"}
		message.Tags_enabled = append(message.Tags_enabled, tag_enabled{Tag: &tag, Enabled: i%2 == 0, BackRef: &message})
	}
	var rendered bytes.Buffer
	if err := tmpl.ExecuteTemplate(&rendered, "base/doc.tmpl", message); err != nil {
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
	tagButtons, actionButtons, thumbnails := 0, 0, 0
	var mobileDate *htmlparser.Node
	actionIcons := 0
	var visit func(*htmlparser.Node)
	visit = func(node *htmlparser.Node) {
		if node.Type == htmlparser.ElementNode {
			if id := attribute(node, "id"); id != "" {
				if elements[id] != nil {
					t.Errorf("duplicate message element ID %q", id)
				}
				elements[id] = node
			}
			if node.Data == "button" {
				if attribute(node, "type") != "button" || attribute(node, "aria-label") == "" || attribute(node, "hx-sync") != "#tray-container:drop" || attribute(node, "hx-disabled-elt") != "this" {
					t.Error("message controls must be labelled native buttons coordinated with list swaps")
				}
				if attribute(node, "class") == "doc-entry-tagview-segment" {
					tagButtons++
				} else {
					actionButtons++
					if attribute(node, "hx-post") == "/tray/doc-delete" && attribute(node, "hx-target") != "closest .doc-entry" {
						t.Error("Delete must still replace only its message row")
					}
				}
			}
			if node.Data == "time" && attribute(node, "class") == "doc-entry-mobile-date" {
				mobileDate = node
			}
			if strings.Contains(attribute(node, "class"), "doc-entry-action-icon") {
				actionIcons++
				if node.Data != "span" || attribute(node, "aria-hidden") != "true" || node.Parent.Data != "button" {
					t.Error("shared action icons must remain decorative children of the labelled native buttons")
				}
			}
			if node.Data == "img" {
				if attribute(node, "src") == "/media/photo.jpg" {
					t.Error("responsive attachment cards must never load originals as previews")
				}
				if attribute(node, "src") == "/media/photo.jpg.thumb.png" {
					thumbnails++
				}
			}
			if node.Data == "a" && attribute(node, "class") == "doc-entry-file-download" {
				if attribute(node, "href") != "/media/photo.jpg" || attribute(node, "aria-label") != "Open Photo & <notes>.jpg" {
					t.Error("attachment links must preserve their original target and untruncated accessible filename")
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	if tagButtons != 20 || actionButtons != 2 || thumbnails != 1 {
		t.Error("responsive cards must retain every tag, both actions and the generated thumbnail")
	}
	if mobileDate == nil {
		t.Fatal("mobile cards must retain a timestamp beside their actions")
	}
	if attribute(mobileDate.Parent, "class") != "doc-entry-button debug" || attribute(mobileDate, "datetime") != "2000-10-03T14:05:59Z" || attribute(mobileDate, "aria-label") != message.Date || attribute(mobileDate, "title") != message.Date {
		t.Error("the compact footer timestamp must preserve full accessible and machine-readable dates")
	}
	if actionIcons != 2 {
		t.Error("both message actions must include exactly one shared icon")
	}
	if strings.Contains(rendered.String(), "doc-entry-action-mobile") || strings.Contains(rendered.String(), "doc-entry-action-desktop") {
		t.Error("message actions must not duplicate icons for desktop and mobile")
	}
	preview := elements["doc-webpreviews-42"]
	if preview == nil || attribute(preview, "class") != "doc-entry-web-previews" || attribute(preview, "hx-get") != "/tray/doc-preview/42" || attribute(preview, "hx-trigger") != "every 1s" || attribute(preview, "hx-swap") != "outerHTML" {
		t.Error("responsive previews must preserve their polling ID, endpoint and fragment swap")
	}
	message.Tags_enabled = nil
	rendered.Reset()
	if err := tmpl.ExecuteTemplate(&rendered, "base/doc-entry.tmpl", message); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.String(), `class="doc-entry-tagview `) {
		t.Error("messages without tags must not retain an empty tag strip")
	}
	message.Date = "unknown <date>"
	rendered.Reset()
	if err := tmpl.ExecuteTemplate(&rendered, "base/doc-entry.tmpl", message); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.String(), "<date>") || strings.Contains(rendered.String(), "datetime=") || !strings.Contains(rendered.String(), "unknown &lt;date&gt;") {
		t.Error("legacy timestamps must remain safely visible without invalid datetime attributes")
	}
}

func TestMessageStarButtonTemplate(t *testing.T) {
	tmpl, err := template.ParseFiles("templates/base/doc.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	for _, starred := range []bool{false, true} {
		var rendered bytes.Buffer
		if err := tmpl.ExecuteTemplate(&rendered, "base/doc-star.tmpl", post{DocID: 42, Starred: starred}); err != nil {
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
		buttons, actionIcons := 0, 0
		var visit func(*htmlparser.Node)
		visit = func(node *htmlparser.Node) {
			if node.Type == htmlparser.ElementNode && strings.Contains(attribute(node, "class"), "doc-entry-action-icon") {
				actionIcons++
				symbol := "☆"
				if starred {
					symbol = "★"
				}
				if node.Data != "span" || attribute(node, "aria-hidden") != "true" || node.FirstChild == nil || node.FirstChild.Data != symbol {
					t.Error("star fragments must retain the shared decorative star icon in both states")
				}
			}
			if node.Type == htmlparser.ElementNode && node.Data == "button" {
				buttons++
				if attribute(node, "id") != "doc-star-42" || attribute(node, "type") != "button" || attribute(node, "aria-label") != "Star message 42" || attribute(node, "aria-pressed") != fmt.Sprint(starred) {
					t.Error("star refreshes must retain a stable ID, label and current pressed state")
				}
				if attribute(node, "hx-post") != "/tray/doc-star" || attribute(node, "hx-target") != "closest .doc-entry-button-fav" || attribute(node, "hx-swap") != "outerHTML" || attribute(node, "hx-sync") != "#tray-container:drop" || attribute(node, "hx-vals") != `{"id":42}` {
					t.Error("star refreshes must preserve the coordinated favourite-only swap")
				}
				if node.Parent.Data != "div" || strings.Contains(attribute(node.Parent, "class"), "starred") != starred {
					t.Error("star response must be the favourite wrapper with its current visual state")
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
		}
		visit(document)
		if buttons != 1 || actionIcons != 1 || strings.Contains(rendered.String(), "doc-entry-container") {
			t.Error("star response must contain only its button wrapper, not a full message")
		}
	}
}

func TestMessageTagButtonTemplate(t *testing.T) {
	tmpl, err := template.ParseFiles("templates/base/doc.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		messageID int
		name      string
		symbol    string
		assigned  bool
	}{
		{messageID: 42, name: "Research & <notes>", symbol: "👩🏽‍💻", assigned: true},
		{messageID: 99, name: "Research & <notes>", symbol: "👩🏽‍💻"},
		{messageID: 42, name: " ", assigned: true},
	} {
		message := post{DocID: test.messageID}
		tag := tag{ID: "reading", Name: test.name, Sym: test.symbol, Enabled: !test.assigned}
		var rendered bytes.Buffer
		if err := tmpl.ExecuteTemplate(&rendered, "base/doc-tagbar-segments.tmpl", tag_enabled{Tag: &tag, Enabled: test.assigned, BackRef: &message}); err != nil {
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
		buttons := 0
		var visit func(*htmlparser.Node)
		visit = func(node *htmlparser.Node) {
			if node.Type == htmlparser.ElementNode && node.Data == "button" {
				buttons++
				if attribute(node, "id") != fmt.Sprintf("doc-tag-%d-reading", test.messageID) || attribute(node, "class") != "doc-entry-tagview-segment" || attribute(node, "type") != "button" {
					t.Error("tag fragments must remain a native button with a message-specific stable ID")
				}
				if attribute(node, "aria-label") != tag.DisplayName() || attribute(node, "title") != tag.DisplayName() || attribute(node, "aria-pressed") != fmt.Sprint(test.assigned) {
					t.Error("accessible tag name, hover title and pressed state must describe assignment, not the browsing filter")
				}
				if attribute(node, "hx-post") != "/tray/post-tag" || attribute(node, "hx-target") != "closest .doc-entry-tagview-segment" || attribute(node, "hx-swap") != "outerHTML" || attribute(node, "hx-sync") != "#tray-container:drop" {
					t.Error("tag refreshes must still swap only their coordinated button")
				}
				var values struct {
					ID  int    `json:"id"`
					Tag string `json:"tag"`
				}
				if err := json.Unmarshal([]byte(attribute(node, "hx-vals")), &values); err != nil || values.ID != test.messageID || values.Tag != tag.ID {
					t.Error("tag refreshes must send the same message and stable tag ID")
				}
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
		}
		visit(document)
		if buttons != 1 || strings.Contains(rendered.String(), "<notes>") {
			t.Error("tag fragments must contain one button and safely escape tag names")
		}
		if strings.Contains(rendered.String(), "doc-entry-tag-name") || strings.Contains(rendered.String(), "doc-entry-tag-selection") {
			t.Error("message tags must retain compact symbol-only segments, not named chips")
		}
		if test.symbol == "" && !strings.Contains(rendered.String(), ">#</span>") {
			t.Error("tags without emoji must retain a visible desktop symbol")
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
			if attr.Key == "autofocus" {
				t.Errorf("%s must not summon a phone keyboard through HTML autofocus", id)
			}
		}
	}
	if attribute(elements["docUpload-text"], "enterkeyhint") != "enter" {
		t.Error("phone keyboards must offer Return rather than advertise automatic sending")
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
		var navigationButtons []*htmlparser.Node
		navigationLinks := make(map[string][]string)
		closeButtons := 0
		var visit func(*htmlparser.Node)
		visit = func(node *htmlparser.Node) {
			if node.Type == htmlparser.ElementNode {
				id := attribute(node, "id")
				if id != "" {
					if elements[id] != nil {
						t.Errorf("duplicate tray element ID %q", id)
					}
					elements[id] = node
				}
				if attribute(node, "class") == "tray-navigation-toggle" {
					navigationButtons = append(navigationButtons, node)
				}
				if attribute(node, "popovertarget") == "tray-navigation" {
					if node.Data != "button" || attribute(node, "type") != "button" || attribute(node, "aria-label") == "" || attribute(node, "hx-post") != "" {
						t.Error("navigation controls must be labelled native buttons, not requests or form submissions")
					}
					if attribute(node, "popovertargetaction") == "hide" {
						closeButtons++
					}
				}
				if node.Data == "a" {
					for parent := node.Parent; parent != nil; parent = parent.Parent {
						if id := attribute(parent, "id"); id == "header" || id == "tray-navigation" {
							navigationLinks[id] = append(navigationLinks[id], attribute(node, "href"))
							break
						}
					}
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
		for _, id := range []string{"page-container", "header", "footer", "tray-container", "workspace-container", "doc-container", "uploadform", "tray-navigation"} {
			if elements[id] == nil {
				t.Fatalf("tray element %q missing (tag edit: %t)", id, tagEdit)
			}
		}
		if composerCount != 1 {
			t.Errorf("tray contains %d composers, want one", composerCount)
		}
		menu := elements["tray-navigation"]
		if menu.Data != "nav" || attribute(menu, "popover") != "auto" || attribute(menu, "aria-label") == "" || menu.Parent != elements["page-container"] {
			t.Error("navigation must be a named native popover outside replaceable fragments and the composer")
		}
		wantNavigationButtons := 2 // Collapsed filter row and expanded filter footer.
		if tagEdit {
			wantNavigationButtons = 1 // Editor toolbar.
		}
		if len(navigationButtons) != wantNavigationButtons || closeButtons != 1 {
			t.Error("navigation must remain available in every filter/editor state with one persistent close control")
		}
		for _, button := range navigationButtons {
			if attribute(button, "popovertarget") != "tray-navigation" || attribute(button, "aria-controls") != "tray-navigation" || button.Parent.Data == "button" {
				t.Error("navigation must be a separate control targeting the persistent popover")
			}
		}
		wantLinks := []string{"/", "/tray", "/about", "/login", "/logout"}
		if !reflect.DeepEqual(navigationLinks["header"], wantLinks) || !reflect.DeepEqual(navigationLinks["tray-navigation"], wantLinks) {
			t.Error("desktop and mobile navigation must preserve the same existing routes")
		}
		if tagEdit && attribute(elements["tag-editor-form"], "hx-disabled-elt") != "#tag-editor-form button:not(.tray-navigation-toggle), #tag-editor-form input" {
			t.Error("editor requests must leave navigation available")
		}
		for _, id := range []string{"header", "tray-container", "footer"} {
			if elements[id].Parent != elements["page-container"] {
				t.Errorf("%s must participate directly in the shared page layout", id)
			}
		}
		if elements["uploadform"].Parent != elements["tray-container"] || elements["workspace-container"].Parent != elements["tray-container"] {
			t.Error("composer must be a sibling of the replaceable workspace")
		}
		if elements["delete-undo"] != nil || elements["delete-undo-button"] != nil {
			t.Error("tray must not retain the global Undo bar")
		}
		for _, fragment := range []string{"posts/workspace-container.tmpl", "base/doc-list.tmpl"} {
			rendered.Reset()
			if err := tmpl.ExecuteTemplate(&rendered, fragment, profile); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rendered.String(), `id="uploadform"`) || strings.Contains(rendered.String(), `id="docUpload-text"`) {
				t.Errorf("%s recreates the composer and would lose its draft", fragment)
			}
			if strings.Contains(rendered.String(), `id="tray-navigation"`) {
				t.Errorf("%s must not recreate the persistent navigation popover", fragment)
			}
		}
	}
}

func TestResponsivePageTemplates(t *testing.T) {
	tmpl, err := template.ParseGlob("templates/*/*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		data    any
		content string
		tray    bool
	}{
		{name: "posts/tray.tmpl", data: profile_data{}, content: "tray-container", tray: true},
		{name: "posts/hello.tmpl", data: "Test user", content: "welcome-container"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var rendered bytes.Buffer
			if err := tmpl.ExecuteTemplate(&rendered, test.name, test.data); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(rendered.String(), "<!DOCTYPE html>") {
				t.Error("full pages must use standards-mode HTML")
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
			viewportCount := 0
			var body *htmlparser.Node
			var visit func(*htmlparser.Node)
			visit = func(node *htmlparser.Node) {
				if node.Type == htmlparser.ElementNode {
					if node.Data == "body" {
						body = node
					}
					if id := attribute(node, "id"); id != "" {
						elements[id] = node
					}
					if node.Data == "meta" && attribute(node, "name") == "viewport" {
						viewportCount++
						content := attribute(node, "content")
						settings := make(map[string]string)
						for _, setting := range strings.Split(content, ",") {
							key, value, _ := strings.Cut(strings.TrimSpace(setting), "=")
							settings[key] = value
						}
						if settings["width"] != "device-width" || settings["initial-scale"] != "1" || settings["interactive-widget"] != "resizes-content" {
							t.Error("viewport must use device width and request keyboard-aware resizing")
						}
						for _, restricted := range []string{"user-scalable", "maximum-scale", "minimum-scale"} {
							if _, present := settings[restricted]; present {
								t.Error("responsive pages must not restrict browser zoom")
							}
						}
					}
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					visit(child)
				}
			}
			visit(document)
			if viewportCount != 1 {
				t.Errorf("full page has %d viewport declarations, want one", viewportCount)
			}
			if body == nil || (attribute(body, "class") == "tray-page") != test.tray {
				t.Fatal("only the tray page should use the bounded tray layout and mobile footer rule")
			}
			for _, id := range []string{"page-container", "header", "footer", test.content} {
				if elements[id] == nil {
					t.Fatalf("page element %q missing", id)
				}
			}
			for _, id := range []string{"header", test.content, "footer"} {
				if elements[id].Parent != elements["page-container"] || attribute(elements[id], "style") != "" {
					t.Errorf("%s must use the shared shell without inline layout overrides", id)
				}
			}
			if elements["header"].Data != "nav" || attribute(elements["header"], "aria-label") == "" {
				t.Error("shared navigation must have a semantic element and accessible name")
			}
		})
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
	for i := 4; i < 20; i++ {
		tags = append(tags, tag{ID: fmt.Sprintf("tag-%d", i), Nr: fmt.Sprint(i), Name: fmt.Sprintf("Tag %d", i), Color: "#335599"})
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
			var summarySegments []*htmlparser.Node
			var visit func(*htmlparser.Node)
			visit = func(node *htmlparser.Node) {
				if node.Type == htmlparser.ElementNode {
					if id := attribute(node, "id"); id != "" {
						if elements[id] != nil {
							t.Errorf("duplicate filter element ID %q", id)
						}
						elements[id] = node
					}
					if attribute(node, "class") == "tag-filter-summary-segment" {
						summarySegments = append(summarySegments, node)
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
			if attribute(elements["tag-container"], "data-tag-mode") != "filter" {
				t.Error("browsing panels must expose their mode for scoped scroll preservation")
			}
			if attribute(elements["tag-container"], "data-filters-open") != "false" {
				t.Error("mobile browsing panels must start collapsed")
			}
			for _, id := range []string{"tag-filter-disclosure", "tag-filter-collapse"} {
				button := elements[id]
				if button == nil {
					t.Fatalf("disclosure button %q missing", id)
				}
				if button.Data != "button" || attribute(button, "type") != "button" || attribute(button, "aria-controls") != "tag-filter-content" || attribute(button, "hx-post") != "" {
					t.Errorf("%s must control disclosure locally, not mutate filters or submit a request", id)
				}
			}
            if attribute(elements["tag-filter-disclosure"], "aria-expanded") != "false" || elements["tag-filter-content"] == nil {
                t.Error("disclosure must expose its initial state and a real control target")
            }
			if attribute(elements["tag-filter-collapse"].Parent, "class") != "tag-filter-footer" || attribute(elements["tag-filter-collapse"].Parent.Parent, "id") != "tag-filter-content" {
				t.Error("collapse must remain outside the filter controls' scroll area")
			}
			if strings.Contains(attribute(elements["tag-filter-disclosure"], "aria-label"), "active") != test.clear || attribute(elements["tag-filter-summary-star"], "data-selected") != test.starred {
				t.Error("collapsed summary must describe active filters and the current starred-only state")
			}
			disclosure := elements["tag-filter-disclosure"]
			if !strings.HasPrefix(attribute(disclosure, "aria-label"), "Message filters") {
				t.Error("label-free disclosure must retain its accessible name")
			}
			for child := disclosure.FirstChild; child != nil; child = child.NextSibling {
				if (child.Type == htmlparser.ElementNode && attribute(child, "aria-hidden") != "true") || (child.Type == htmlparser.TextNode && strings.TrimSpace(child.Data) != "") {
					t.Error("collapsed disclosure must contain only decorative indicators, not a visible text label")
				}
			}
			if len(summarySegments) != len(test.profile.Tags) {
				t.Fatal("collapsed summary must retain every tag, including with 20 tags or none")
			}
			for i, segment := range summarySegments {
				if segment.Data != "span" || attribute(segment.Parent, "aria-hidden") != "true" || attribute(segment, "data-selected") != fmt.Sprint(test.profile.Tags[i].Enabled) || !strings.Contains(attribute(segment, "style"), test.profile.Tags[i].Color) {
					t.Error("summary fragments must be decorative and reflect browsing-filter selection and colour")
				}
			}
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
			if attribute(manage, "hx-post") != "/tray/tag-edit" || attribute(manage, "hx-target") != "#tag-container" || attribute(manage, "hx-sync") != "#tray-container:drop" {
				t.Error("Manage tags must open the existing editor without replacing the composer or racing a shared request")
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
		rows, removeButtons, fieldLabels := 0, 0, 0
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
					if attribute(node, "type") != "hidden" && (node.Parent.Data != "label" || attribute(node.Parent, "for") != attribute(node, "id")) {
						t.Error("editable inputs need associated field labels when narrow layouts hide column headings")
					}
					if attribute(node, "maxlength") != "" {
						t.Error("emoji inputs must not truncate multi-codepoint emoji")
					}
				}
				if attribute(node, "class") == "tag-editor-row" {
					rows++
				}
				if attribute(node, "class") == "tag-editor-label" {
					fieldLabels++
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
		if attribute(elements["tag-container"], "data-tag-mode") != "edit" {
			t.Error("editor panels must expose their mode for narrow workspace layout and scoped scroll preservation")
		}
		if elements["tag-filter-disclosure"] != nil || elements["tag-filter-collapse"] != nil {
			t.Error("the editor must retain explicit Save/Cancel, not filter-panel dismissal controls")
		}
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
		if rows != len(draft) || removeButtons != len(draft) || fieldLabels != 3*len(draft) {
			t.Error("each tag must have one row, three field labels and one removal control")
		}
		if parsed := tagsFromMultiform(form, tags); len(draft) > 0 && !reflect.DeepEqual(parsed, draft) {
			t.Error("editor round-trips must preserve names, emoji, IDs and filter selections without repeated escaping")
		}
		if len(draft) == 0 && !strings.Contains(rendered.String(), "No tags yet") {
			t.Error("empty editors must explain how to add a tag")
		}
	}
}

func TestDeletedPostLifecycle(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "attachment.txt")
	if err := os.WriteFile(filename, []byte("original contents"), 0600); err != nil {
		t.Fatal(err)
	}
	message := post{DocID: 9, Title: "Keep &amp; restore", Date: "original date", Starred: true,
		Tags: []string{"reading"}, Files: []docentry_file{{Url: "/media/attachment.txt"}},
		Webpreview: []previewbuilder.URLPreview{{ID: "preview", Title: "Existing preview"}}}
	want := message
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.FixedZone("test", 3600))
	message.MarkDeleted(at)
	message.MarkDeleted(at.Add(time.Minute))
	if message.DeletedAt == nil || !message.DeletedAt.Equal(at) || message.DeletedAt.Location() != time.UTC {
		t.Fatal("Delete must retain its first timestamp in UTC")
	}
	encoded, err := json.Marshal(message)
	if err != nil || !strings.Contains(string(encoded), `"deleted_at"`) {
		t.Fatal("deletion marker must be persisted")
	}
	profile := profile_data{Posts: []post{{DocID: 7, Title: "Live message"}, message}}
	if rendered := render_all(profile); len(rendered.Posts) != 1 || rendered.Posts[0].DocID != 7 {
		t.Error("ordinary rendering must hide marked messages")
	}
	if len(profile.Posts) != 2 || get_data_new_id(&profile.Posts) != 10 {
		t.Error("rendering must preserve marked records and their reserved IDs")
	}
	if contents, err := os.ReadFile(filename); err != nil || string(contents) != "original contents" {
		t.Error("Delete and rendering must leave attachment contents untouched")
	}
	if err := message.Restore(directory, at); err != nil || !reflect.DeepEqual(message, want) {
		t.Error("Undo must restore the same message with metadata, attachments and previews intact")
	}
	encoded, err = json.Marshal(message)
	if err != nil || strings.Contains(string(encoded), `"deleted_at"`) {
		t.Error("restoring must remove the persisted deletion marker")
	}
	var legacy post
	if err := json.Unmarshal([]byte(`{"id":1,"title":"old message"}`), &legacy); err != nil || legacy.DeletedAt != nil {
		t.Error("existing messages without a deletion marker must remain live")
	}
}

func TestPurgeDeletedPostsRetry(t *testing.T) {
	directory := t.TempDir()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, basename := range []string{"active.bin", "deleted.bin", "blocked.bin"} {
		if err := os.WriteFile(filepath.Join(directory, basename), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(thumbnail.Path(filepath.Join(directory, "deleted.bin")), []byte("thumbnail"), 0600); err != nil {
		t.Fatal(err)
	}
	// A nonempty directory produces a deterministic removal failure, even as root.
	blockedThumbnail := thumbnail.Path(filepath.Join(directory, "blocked.bin"))
	if err := os.Mkdir(blockedThumbnail, 0700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(blockedThumbnail, "blocker")
	if err := os.WriteFile(blocker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	profile := profile_data{Posts: []post{
		{DocID: 1, Files: []docentry_file{{Url: "/media/active.bin"}}},
		{DocID: 2, DeletedAt: &at},
		{DocID: 3, DeletedAt: &at, Files: []docentry_file{{Url: "/media/deleted.bin"}}},
		{DocID: 4, DeletedAt: &at, Files: []docentry_file{{Url: "/media/blocked.bin"}}},
	}}
	purgeDeletedPosts(&profile, directory, logger)
	if len(profile.Posts) != 2 || profile.Posts[0].DocID != 1 || profile.Posts[1].DocID != 4 || profile.Posts[1].DeletedAt == nil {
		t.Fatal("cleanup must drop completed records but retain the marked failed record")
	}
	if _, err := os.Stat(filepath.Join(directory, "active.bin")); err != nil {
		t.Error("cleanup must not touch active uploads")
	}
	for _, filename := range []string{filepath.Join(directory, "deleted.bin"), thumbnail.Path(filepath.Join(directory, "deleted.bin"))} {
		if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
			t.Error("successful cleanup must remove original and thumbnail")
		}
	}
	if err := profile.Posts[1].Restore(directory, at); !errors.Is(err, errUndoUnavailable) || profile.Posts[1].DeletedAt == nil {
		t.Error("Undo must not restore an attachment message after partial cleanup removed its original")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	purgeDeletedPosts(&profile, directory, logger)
	if len(profile.Posts) != 1 || profile.Posts[0].DocID != 1 {
		t.Error("retry must finish cleanup, tolerating originals already removed")
	}
}

func TestFullTrayLoadPurgesDeletedPosts(t *testing.T) {
	previous := DATA_BASE_PATH
	DATA_BASE_PATH = t.TempDir()
	t.Cleanup(func() { DATA_BASE_PATH = previous })
	const subject = "test-user"
	directory := filepath.Join(DATA_BASE_PATH, "uploads", subject)
	for _, folder := range []string{filepath.Join(DATA_BASE_PATH, "data"), directory} {
		if err := os.MkdirAll(folder, 0700); err != nil {
			t.Fatal(err)
		}
	}
	filename := filepath.Join(directory, "attachment.txt")
	for _, name := range []string{filename, thumbnail.Path(filename)} {
		if err := os.WriteFile(name, []byte("keep until reload"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().UTC()
	profile := profile_data{Posts: []post{
		{DocID: 1, Title: "Live", Type: doctype_mesage},
		{DocID: 2, Title: "Deleted", Type: doctype_file, DeletedAt: &at,
			Files: []docentry_file{{Url: "/media/attachment.txt", OrgName: "attachment.txt"}}},
	}}
	set_data(profile, subject)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	if read := get_data(subject); len(read.Posts) != 2 || read.Posts[1].DeletedAt == nil {
		t.Fatal("ordinary reads must retain deletion markers")
	}
	if read := loadTrayProfile(subject, false, logger); len(read.Posts) != 2 {
		t.Fatal("HTMX tray loads must not purge marked posts")
	}
	for _, name := range []string{filename, thumbnail.Path(filename)} {
		if _, err := os.Stat(name); err != nil {
			t.Error("ordinary/HTMX reads must leave deleted uploads in place")
		}
	}
	if loaded := loadTrayProfile(subject, true, logger); len(loaded.Posts) != 1 || loaded.Posts[0].DocID != 1 {
		t.Fatal("full tray load must synchronously purge marked messages")
	}
	if saved := get_data(subject); len(saved.Posts) != 1 {
		t.Error("completed cleanup must be persisted")
	}
	for _, name := range []string{filename, thumbnail.Path(filename)} {
		if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
			t.Error("full page load must remove originals and thumbnails")
		}
	}
}

func TestDeletedPostsSkipPreviewQueue(t *testing.T) {
	queue := &previewJobQueue{jobs: make(chan previewJob, 2), activeJobs: make(map[string]bool)}
	at := time.Now().UTC()
	profile := profile_data{Posts: []post{
		{DocID: 1, Webpreview: []previewbuilder.URLPreview{{ID: "live", Pending: true}}},
		{DocID: 2, DeletedAt: &at, Webpreview: []previewbuilder.URLPreview{{ID: "deleted", Pending: true}}},
	}}
	queue.enqueuePendingPreviews("test-user", profile)
	if len(queue.jobs) != 1 {
		t.Fatal("marked messages must not enqueue preview work")
	}
	if job := <-queue.jobs; job.postID != 1 {
		t.Error("only the live message should be queued")
	}
}

func TestWriteProfileFile(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "profile.json")
	for _, contents := range []string{`{"version":1}`, `{"version":2}`} {
		if err := writeProfileFile(filename, []byte(contents)); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(filename); err != nil || string(got) != contents {
			t.Error("atomic replacement must write the complete new profile")
		}
	}
	blocked := filepath.Join(directory, "blocked.json")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeProfileFile(blocked, []byte("cannot replace a directory")); err == nil {
		t.Error("failed replacement must report an error")
	}
	if got, err := os.ReadFile(filename); err != nil || string(got) != `{"version":2}` {
		t.Error("failed writes must not damage an existing profile")
	}
	if files, err := filepath.Glob(filepath.Join(directory, "*.tmp")); err != nil || len(files) != 0 {
		t.Error("completed and failed writes must clean up temporary files")
	}
}

func TestRestoreDeletionIdentityAndErrors(t *testing.T) {
	directory := t.TempDir()
	originalDeletion := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	laterDeletion := originalDeletion.Add(time.Minute)
	message := post{DocID: 9, Title: "Different message with reused ID", DeletedAt: &laterDeletion}
	if err := message.Restore(directory, originalDeletion); !errors.Is(err, errUndoUnavailable) || message.DeletedAt == nil {
		t.Error("stale Undo must not restore a different deletion with the same numeric ID")
	}
	if err := message.Restore(directory, laterDeletion); err != nil || message.DeletedAt != nil {
		t.Error("the current deletion timestamp must allow restoration")
	}
	if err := message.Restore(directory, originalDeletion); !errors.Is(err, errUndoUnavailable) {
		t.Error("stale Undo must not operate on a live message")
	}
	message.DeletedAt = &laterDeletion
	message.Files = []docentry_file{{Url: "/media/missing.txt"}}
	if err := message.Restore(directory, laterDeletion); !errors.Is(err, errUndoUnavailable) {
		t.Error("missing originals must report permanently unavailable Undo")
	}
	// ENAMETOOLONG is a deterministic operational Stat error, unlike missing files.
	message.Files[0].Url = "/media/" + strings.Repeat("x", 300)
	if err := message.Restore(directory, laterDeletion); err == nil || errors.Is(err, errUndoUnavailable) || message.DeletedAt == nil {
		t.Error("operational attachment errors must retain the marker and remain retryable")
	}
}

func TestPerMessageUndoTemplate(t *testing.T) {
	tmpl, err := template.ParseFiles("templates/base/doc-list.tmpl", "templates/base/doc.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	first := time.Date(2026, 9, 30, 12, 0, 0, 123456789, time.UTC)
	second := first.Add(time.Minute)
	profile := profile_data{Only_favorites: true, Posts: []post{
		{DocID: 1, Title: "Visible message", Starred: true},
		{DocID: 2, Title: "Secret deleted text", DeletedAt: &first},
		{DocID: 3, Title: "Other deleted text", DeletedAt: &second,
			Files: []docentry_file{{Url: "/media/private.txt", OrgName: "secret-file.txt"}}},
	}}
	view := renderPosts(profile, true)
	if len(view.Posts) != 3 || view.Posts[1].DocID != 2 || view.Posts[2].DocID != 3 {
		t.Fatal("partial refreshes must retain individual Removed rows in original order")
	}
	var rendered bytes.Buffer
	if err := tmpl.ExecuteTemplate(&rendered, "base/doc-list.tmpl", view); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"Secret deleted text", "Other deleted text", "secret-file.txt", "/media/private.txt"} {
		if strings.Contains(rendered.String(), secret) {
			t.Error("Removed rows must not display deleted text or attachment contents")
		}
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
	undo := make(map[int]string)
	var visit func(*htmlparser.Node)
	visit = func(node *htmlparser.Node) {
		if node.Type == htmlparser.ElementNode && attribute(node, "class") == "doc-entry-undo" {
			var values struct {
				ID        int    `json:"id"`
				DeletedAt string `json:"deleted_at"`
			}
			if err := json.Unmarshal([]byte(attribute(node, "hx-vals")), &values); err != nil {
				t.Fatal(err)
			}
			undo[values.ID] = values.DeletedAt
			if attribute(node, "hx-post") != "/tray/doc-restore" || attribute(node, "hx-target") != "closest .doc-entry" || attribute(node, "hx-swap") != "outerHTML" || attribute(node, "hx-sync") != "#tray-container:drop" {
				t.Error("individual Undo must restore just its row with coordinated requests")
			}
			if node.Parent.Data != "li" || attribute(node.Parent, "class") != "doc-entry doc-type-removed" || attribute(node, "aria-label") == "" {
				t.Error("labelled Undo button must live directly in its Removed row")
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	if len(undo) != 2 || undo[2] != first.Format(time.RFC3339Nano) || undo[3] != second.Format(time.RFC3339Nano) {
		t.Error("each Removed row must carry its own ID and exact deletion timestamp")
	}
	// Restore the second deletion first, without affecting the first deletion.
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "private.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := profile.Posts[2].Restore(directory, second); err != nil {
		t.Fatal(err)
	}
	rendered.Reset()
	if err := tmpl.ExecuteTemplate(&rendered, "base/doc-entry.tmpl", render_post(profile.Posts[2])); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.String(), "doc-entry-container") || strings.Contains(rendered.String(), "doc-type-removed") || !strings.Contains(rendered.String(), "Other deleted text") {
		t.Error("restore response must be the live list item, not a nested wrapper or a whole list")
	}
	if profile.Posts[1].DeletedAt == nil {
		t.Error("individual restoration must not alter other removed messages")
	}
}

func TestReadProfileAndStartupIsolation(t *testing.T) {
	previous := DATA_BASE_PATH
	DATA_BASE_PATH = t.TempDir()
	t.Cleanup(func() { DATA_BASE_PATH = previous })
	directory := filepath.Join(DATA_BASE_PATH, "data")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte(`{"posts":`)
	if err := os.WriteFile(filepath.Join(directory, "corrupt.json"), corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProfile("corrupt"); err == nil {
		t.Error("invalid profiles must report an error rather than returning usable partial data")
	}
	if _, err := readProfile("absent"); err != nil {
		t.Error("new users without a saved profile must still load normally")
	}
	at := time.Now().UTC()
	large := profile_data{Posts: []post{{DocID: 1, DeletedAt: &at, Title: template.HTML(strings.Repeat("x", 1024*1024+10))}}}
	set_data(large, "large")
	if loaded, err := readProfile("large"); err != nil || len(loaded.Posts) != 1 || len(loaded.Posts[0].Title) != len(large.Posts[0].Title) {
		t.Fatal("valid profiles larger than 1 MiB must load without truncation")
	}
	set_data(profile_data{Posts: []post{{DocID: 2, Webpreview: []previewbuilder.URLPreview{{ID: "live", Pending: true}}}}}, "good")
	queue := &previewJobQueue{jobs: make(chan previewJob, 2), activeJobs: make(map[string]bool), logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	resumePendingPreviews(queue)
	if len(queue.jobs) != 1 {
		t.Fatal("startup must skip invalid/deleted profiles while recovering healthy preview jobs")
	}
	if job := <-queue.jobs; job.subject != "good" {
		t.Error("healthy profiles must continue to recover despite a corrupt neighbour")
	}
	if loaded, err := readProfile("large"); err != nil || loaded.Posts[0].DeletedAt == nil {
		t.Error("startup recovery must not purge marked posts")
	}
	if contents, err := os.ReadFile(filepath.Join(directory, "corrupt.json")); err != nil || !bytes.Equal(contents, corrupt) {
		t.Error("invalid profiles must not be overwritten")
	}
}
