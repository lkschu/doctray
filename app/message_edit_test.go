package main

import (
	"bytes"
	"encoding/json"
	"html"
	"html/template"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"doctray/internal/previewbuilder"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func messageEditTestRouter(t *testing.T) (*gin.Engine, *previewJobQueue) {
	t.Helper()
	previousDirectory, previousMode := DATA_BASE_PATH, gin.Mode()
	DATA_BASE_PATH = t.TempDir()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() {
		DATA_BASE_PATH = previousDirectory
		gin.SetMode(previousMode)
	})
	for _, folder := range []string{"data", "uploads/editor"} {
		if err := os.MkdirAll(filepath.Join(DATA_BASE_PATH, folder), 0700); err != nil {
			t.Fatal(err)
		}
	}
	tmpl, err := template.ParseFiles("templates/base/doc.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.SetHTMLTemplate(tmpl)
	router.Use(sessions.Sessions("test", cookie.NewStore([]byte(strings.Repeat("test-key", 4)))))
	// Exercise the real session-scoped handlers without an OIDC server.
	router.Use(func(c *gin.Context) { sessions.Default(c).Set("sub", "editor"); c.Next() })
	queue := &previewJobQueue{jobs: make(chan previewJob, previewJobQueueSize), activeJobs: make(map[string]bool),
		logger: slog.New(slog.NewJSONHandler(io.Discard, nil))} // No workers/network in tests.
	registerMessageEditing(router.Group("/tray"), queue)
	return router, queue
}

func requestPostEdit(t *testing.T, router *gin.Engine, fields map[string]string, upload bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if upload {
		file, err := form.CreateFormFile("files", "extra.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte("new attachment")); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/tray/doc-update", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestMessageEditReadAndSave(t *testing.T) {
	router, queue := messageEditTestRouter(t)
	text := "**Original** & <notes> https://keep.example/ https://gone.example/"
	keep := previewbuilder.URLPreview{ID: "keep", URL: "https://keep.example/", Title: "Already fetched"}
	message := post{DocID: 0, Title: template.HTML(html.EscapeString(text)), Type: doctype_file,
		Date: "Tue, 03 Oct 2000 14:05:59 GMT", Starred: true, Tags: []string{"reading"},
		Files: []docentry_file{{Url: "/media/notes.txt", OrgName: "notes.txt", ThumbnailURL: "/media/notes.txt.thumb.png"}},
		Webpreview: []previewbuilder.URLPreview{keep, {ID: "gone", URL: "https://gone.example/"}}}
	profile := profile_data{Tags: []tag{{ID: "reading", Nr: "0", Name: "Reading"}}, Posts: []post{message, {DocID: 1, Title: "Untouched"}}}
	set_data(profile, "editor")
	for _, name := range []string{"notes.txt", "notes.txt.thumb.png"} {
		if err := os.WriteFile(filepath.Join(DATA_BASE_PATH, "uploads/editor", name), []byte("original bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	filename := filepath.Join(DATA_BASE_PATH, "data/editor.json")
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tray/doc-edit/0", nil))
	var source struct {
		ID       int    `json:"id"`
		Text     string `json:"text"`
		Revision string `json:"revision"`
		HasFiles bool   `json:"has_files"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &source) != nil || source.ID != 0 || source.Text != text || source.Revision != message.editRevision() || !source.HasFiles {
		t.Fatalf("edit read must return unformatted source text and an attachment-preserving revision: %d", response.Code)
	}
	if after, err := os.ReadFile(filename); err != nil || !bytes.Equal(before, after) || len(queue.jobs) != 0 {
		t.Fatal("opening an edit must not write profile data or enqueue previews")
	}
	// Star/tag changes while editing are unrelated and must be retained by Save.
	current := get_data("editor")
	current.Posts[0].Starred = false
	current.Posts[0].Tags = nil
	set_data(current, "editor")
	response = requestPostEdit(t, router, map[string]string{"id": "0", "revision": source.Revision,
		"title": "  New & <notes>\r\nhttps://keep.example/ https://new.example/  "}, false)
	if response.Code != http.StatusOK {
		t.Fatalf("save status = %d, body = %s", response.Code, response.Body.String())
	}
	saved := get_data("editor")
	updated := saved.Posts[0]
	if len(saved.Posts) != 2 || string(updated.Title) != "New &amp; &lt;notes&gt;\nhttps://keep.example/ https://new.example/" || updated.DocID != message.DocID || updated.Date != message.Date || updated.Type != message.Type || updated.Starred || len(updated.Tags) != 0 || !reflect.DeepEqual(updated.Files, current.Posts[0].Files) || saved.Posts[1].Title != "Untouched" {
		t.Error("Save must change only text/previews, preserving identity, attachments and current metadata")
	}
	if len(updated.Webpreview) != 2 || !reflect.DeepEqual(updated.Webpreview[0], keep) || !updated.Webpreview[1].Pending || updated.Webpreview[1].URL != "https://new.example/" || len(queue.jobs) != 1 {
		t.Error("Save must retain unchanged previews, remove obsolete ones and queue only new pending URLs")
	}
	body := response.Body.String()
	if !strings.Contains(body, `class="doc-entry doc-type-file"`) || strings.Contains(body, `class="doc-entry-container"`) || strings.Contains(body, "Untouched") || strings.Contains(body, "<notes>") || !strings.Contains(body, `src="/media/notes.txt.thumb.png"`) {
		t.Error("Save must return one safe live list item, not a new message/list or original-file preview")
	}
	for _, name := range []string{"notes.txt", "notes.txt.thumb.png"} {
		if data, err := os.ReadFile(filepath.Join(DATA_BASE_PATH, "uploads/editor", name)); err != nil || string(data) != "original bytes" {
			t.Error("text edits must not touch existing uploads or thumbnails")
		}
	}
	// The old edit cannot overwrite a newer save.
	if response := requestPostEdit(t, router, map[string]string{"id": "0", "revision": source.Revision, "title": "Stale edit"}, false); response.Code != http.StatusConflict {
		t.Error("a stale source revision must be rejected")
	}
}

func TestMessageEditErrorsAndAttachmentOnlyText(t *testing.T) {
	router, _ := messageEditTestRouter(t)
	set_data(profile_data{Posts: []post{{DocID: 7, Title: "Other user's message"}}}, "other")
	deletedAt := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name     string
		id       string
		text     string
		files    bool
		deleted  bool
		stale    bool
		upload   bool
		wantCode int
	}{
		{name: "invalid ID", id: "invalid", text: "New", wantCode: http.StatusBadRequest},
		{name: "different subject", id: "7", text: "New", wantCode: http.StatusNotFound},
		{name: "deleted", id: "0", text: "New", deleted: true, wantCode: http.StatusNotFound},
		{name: "stale revision", id: "0", text: "New", stale: true, wantCode: http.StatusConflict},
		{name: "empty without attachments", id: "0", wantCode: http.StatusBadRequest},
		{name: "additional upload", id: "0", text: "New", upload: true, wantCode: http.StatusBadRequest},
		{name: "clear caption", id: "0", files: true, wantCode: http.StatusOK},
		{name: "add caption", id: "0", files: true, text: "A caption", wantCode: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			message := post{DocID: 0, Title: "Original", Date: "original date"}
			if test.files {
				message.Files = []docentry_file{{Url: "/media/existing.txt"}}
				if test.text != "" {
					message.Title = "" // Editing an attachment-only message can add text.
				}
			}
			if test.deleted {
				message.DeletedAt = &deletedAt
			}
			set_data(profile_data{Posts: []post{message}}, "editor")
			filename := filepath.Join(DATA_BASE_PATH, "data/editor.json")
			before, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			revision := message.editRevision()
			if test.stale {
				revision = "outdated"
			}
			response := requestPostEdit(t, router, map[string]string{"id": test.id, "revision": revision, "title": test.text}, test.upload)
			if response.Code != test.wantCode {
				t.Fatalf("status = %d, want %d", response.Code, test.wantCode)
			}
			if test.wantCode != http.StatusOK {
				if after, err := os.ReadFile(filename); err != nil || !bytes.Equal(before, after) {
					t.Error("rejected edits must not write any changes")
				}
			} else if saved := get_data("editor").Posts[0]; string(saved.Title) != test.text || len(saved.Files) != 1 {
				t.Error("caption edits must retain the attachment")
			}
			if test.deleted || test.id != "0" {
				get := httptest.NewRecorder()
				router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/tray/doc-edit/"+test.id, nil))
				if get.Code != test.wantCode || strings.Contains(get.Body.String(), "Other user's message") {
					t.Error("edit reads must validate the ID and remain subject-scoped/live-only")
				}
			}
		})
	}
}

func TestReconcilePostPreviews(t *testing.T) {
	// A final word-boundary assertion must not trim valid trailing URL slashes.
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "https://example.com/", want: "https://example.com/"},
		{input: "https://example.com/article/", want: "https://example.com/article/"},
		{input: "https://example.com/path//", want: "https://example.com/path//"},
		{input: "https://example.com/article/.", want: "https://example.com/article/"},
		{input: "https://example.com/article).", want: "https://example.com/article"},
	} {
		t.Run(test.input, func(t *testing.T) {
			text := "Before " + test.input + " after"
			matches := find_url_in_string([]byte(text))
			if len(matches) != 1 {
				t.Fatalf("URL matches = %d, want one", len(matches))
			}
			if got := text[matches[0][0]:matches[0][1]]; got != test.want {
				t.Errorf("extracted URL = %q, want %q", got, test.want)
			}
		})
	}
	existing := []previewbuilder.URLPreview{
		{ID: "ready", URL: "https://ready.example/", Title: "Cached"},
		{ID: "pending", URL: "https://pending.example/", Pending: true},
		{ID: "removed", URL: "https://removed.example/"},
	}
	result := reconcilePostPreviews("https://ready.example/ https://pending.example/ https://new.example/ https://ready.example/ https://fourth.example/", existing)
	if len(result) != maxPreviewsPerMessage || !reflect.DeepEqual(result[0], existing[0]) || !reflect.DeepEqual(result[1], existing[1]) || result[2].URL != "https://new.example/" || !result[2].Pending || result[2].ID == "" {
		t.Error("preview reconciliation must retain existing cards, deduplicate URLs and cap new cards at three")
	}
	if result := reconcilePostPreviews("No URLs remain", existing); len(result) != 0 {
		t.Error("removed URLs must remove their previews so stale worker completions cannot match them")
	}
	legacy := previewbuilder.URLPreview{URL: "https://legacy.example/", Title: "Cached without an ID"}
	if result := reconcilePostPreviews(legacy.URL, []previewbuilder.URLPreview{legacy}); len(result) != 1 || !reflect.DeepEqual(result[0], legacy) {
		t.Error("unchanged legacy previews must be retained without an unnecessary refetch")
	}
}
