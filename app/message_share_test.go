package main

import (
	"bytes"
	"errors"
	"html/template"
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

	"doctray/internal/requestlog"

	"github.com/gin-gonic/gin"
)

func textSharingTestRouter(t *testing.T, loggedIn bool) (*gin.Engine, *previewJobQueue, *bytes.Buffer) {
	t.Helper()
	previousDirectory := DATA_BASE_PATH
	DATA_BASE_PATH = t.TempDir()
	t.Cleanup(func() { DATA_BASE_PATH = previousDirectory })
	for _, directory := range []string{"data", "uploads/sender"} {
		if err := os.MkdirAll(filepath.Join(DATA_BASE_PATH, directory), 0700); err != nil {
			t.Fatal(err)
		}
	}
	router := webAssetsTestRouter(t)
	tmpl, err := template.ParseGlob("templates/*/*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	router.SetHTMLTemplate(tmpl)
	logs := new(bytes.Buffer)
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	router.Use(requestlog.Middleware(logger))
	queue := &previewJobQueue{jobs: make(chan previewJob, previewJobQueueSize), activeJobs: make(map[string]bool), logger: logger}
	registerTextSharing(router, func(c *gin.Context) (string, error) {
		if !loggedIn {
			return "", errors.New("session expired")
		}
		return "sender", nil
	}, queue)
	return router, queue, logs
}

func multipartShareRequest(t *testing.T, fields map[string]string, file []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if file != nil {
		part, err := form.CreateFormFile("files", "photo.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/tray/share", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	return request
}

func TestSharedMessageText(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields map[string][]string
		want   string
	}{
		{name: "empty"},
		{name: "blank", fields: map[string][]string{"title": {" \r\n "}, "text": {"\t"}}},
		{name: "URL in Android text", fields: map[string][]string{"text": {"https://example.com/"}}, want: "https://example.com/"},
		{name: "distinct fields", fields: map[string][]string{"title": {"Title"}, "text": {"Notes\r\nmore"}, "url": {"https://example.com/"}}, want: "Title\nNotes\nmore\nhttps://example.com/"},
		{name: "exact duplicate fields", fields: map[string][]string{"title": {" Notes "}, "text": {"Notes"}, "url": {"Notes"}}, want: "Notes"},
		{name: "preserve URL embedded in text", fields: map[string][]string{"text": {"Read https://example.com/"}, "url": {"https://example.com/"}}, want: "Read https://example.com/\nhttps://example.com/"},
		{name: "ignore unknown fields", fields: map[string][]string{"subject": {"other"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := sharedMessageText(test.fields); got != test.want {
				t.Errorf("shared text = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTextShareSave(t *testing.T) {
	router, queue, logs := textSharingTestRouter(t, true)
	deletedAt := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	profile := profile_data{Only_favorites: true, Posts: []post{{DocID: 7, Title: "Keep Undo", DeletedAt: &deletedAt}}}
	set_data(profile, "sender")
	set_data(profile_data{Posts: []post{{DocID: 0, Title: "Other account"}}}, "other")
	otherFile := filepath.Join(DATA_BASE_PATH, "data", "other.json")
	otherBefore, err := os.ReadFile(otherFile)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, multipartShareRequest(t, map[string]string{
		"title": "  Private & <notes>  ", "text": "https://example.com/", "url": "https://example.com/",
	}, nil))
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/tray/" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("successful share must redirect to the tray without a repeatable POST: %d", response.Code)
	}
	saved := get_data("sender")
	if len(saved.Posts) != 2 || !saved.Only_favorites || saved.Posts[0].DeletedAt == nil || !saved.Posts[0].DeletedAt.Equal(deletedAt) {
		t.Fatal("sharing must append one message without changing filters or purging Removed records itself")
	}
	message := saved.Posts[1]
	if message.DocID != 8 || message.Type != doctype_mesage || string(message.Title) != "Private &amp; &lt;notes&gt;\nhttps://example.com/" || len(message.Files) != 0 || message.Starred || len(message.Tags) != 0 {
		t.Error("sharing must use ordinary escaped message text, reserved IDs and metadata defaults")
	}
	if _, err := time.Parse(http.TimeFormat, message.Date); err != nil {
		t.Error("shared messages must retain the existing UTC date format")
	}
	if len(message.Webpreview) != 1 || !message.Webpreview[0].Pending || message.Webpreview[0].URL != "https://example.com/" || len(queue.jobs) != 1 {
		t.Fatal("a shared URL must create one normal pending preview/job")
	}
	if job := <-queue.jobs; job.subject != "sender" || job.postID != 8 || job.previewID != message.Webpreview[0].ID {
		t.Error("preview work must remain scoped to the sharing user's new message")
	}
	if otherAfter, err := os.ReadFile(otherFile); err != nil || !bytes.Equal(otherBefore, otherAfter) {
		t.Error("sharing must not alter another user's profile")
	}
	if strings.Contains(logs.String(), "Private") || strings.Contains(logs.String(), "https://example.com/") || strings.Contains(logs.String(), "sender") {
		t.Error("even DEBUG logs must not contain shared content, URLs or user subjects")
	}
	router.GET("/tray/", func(c *gin.Context) { c.String(http.StatusOK, "tray") })
	refresh := httptest.NewRecorder()
	router.ServeHTTP(refresh, httptest.NewRequest(http.MethodGet, response.Header().Get("Location"), nil))
	if refresh.Code != http.StatusOK || len(get_data("sender").Posts) != 2 {
		t.Error("refreshing the redirect destination must not submit the share again")
	}
}

func TestTextShareRejections(t *testing.T) {
	for _, test := range []struct {
		name       string
		loggedIn   bool
		fields     map[string]string
		file       []byte
		malformed  bool
		blockWrite bool
		status     int
	}{
		{name: "expired login", fields: map[string]string{"text": "Private rejected text"}, status: http.StatusUnauthorized},
		{name: "empty", loggedIn: true, fields: map[string]string{"text": " \r\n "}, status: http.StatusBadRequest},
		{name: "unknown fields", loggedIn: true, fields: map[string]string{"subject": "not a message"}, status: http.StatusBadRequest},
		{name: "malformed multipart", loggedIn: true, malformed: true, status: http.StatusBadRequest},
		{name: "oversized", loggedIn: true, fields: map[string]string{"text": strings.Repeat("x", maxSharedTextRequestBytes+1)}, status: http.StatusRequestEntityTooLarge},
		{name: "files not advertised yet", loggedIn: true, fields: map[string]string{"text": "Caption"}, file: []byte("image bytes"), status: http.StatusBadRequest},
		{name: "failed persistence", loggedIn: true, fields: map[string]string{"text": "Private rejected text"}, blockWrite: true, status: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, queue, logs := textSharingTestRouter(t, test.loggedIn)
			set_data(profile_data{Posts: []post{{DocID: 3, Title: "Existing message"}}}, "sender")
			filename := filepath.Join(DATA_BASE_PATH, "data", "sender.json")
			before, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if test.blockWrite {
				if err := os.Mkdir(filename+".tmp", 0700); err != nil {
					t.Fatal(err)
				}
			}
			request := multipartShareRequest(t, test.fields, test.file)
			if test.malformed {
				request = httptest.NewRequest(http.MethodPost, "/tray/share", strings.NewReader("not a multipart body"))
				request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || response.Header().Get("Location") != "" || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), "Share not saved") {
				t.Fatalf("failure must be explicit, uncached and never a successful/login redirect: %d", response.Code)
			}
			if !test.loggedIn && !strings.Contains(response.Body.String(), `href="/login"`) {
				t.Error("expired sharing must offer login and instruct the user to share again")
			}
			if after, err := os.ReadFile(filename); err != nil || !bytes.Equal(before, after) || len(queue.jobs) != 0 {
				t.Error("rejected shares must not persist anything or enqueue preview jobs")
			}
			if strings.Contains(response.Body.String(), "Private rejected text") || strings.Contains(logs.String(), "Private rejected text") || strings.Contains(logs.String(), "sender.json") {
				t.Error("failure pages/logs must not reveal private share content or storage filenames")
			}
		})
	}
}

func TestSharedCreationPreservesComposerAttachments(t *testing.T) {
	router, queue, _ := textSharingTestRouter(t, true)
	set_data(profile_data{Posts: []post{{DocID: 1, Title: "Existing message"}}}, "sender")
	image, err := os.ReadFile("resources/tray_192.png")
	if err != nil {
		t.Fatal(err)
	}
	router.POST("/test-composer", func(c *gin.Context) {
		form, err := c.MultipartForm()
		if err != nil {
			t.Fatal(err)
		}
		defer form.RemoveAll()
		profile, message, err := createMessage(c, "sender", form.Value["title"][0], form.File["files"], queue)
		if err != nil {
			t.Fatal(err)
		}
		renderCreatedPost(c, profile, message)
	})
	request := multipartShareRequest(t, map[string]string{"title": "  Caption & <notes>\r\nnext line  "}, image)
	request.URL.Path = "/test-composer"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	saved := get_data("sender")
	if response.Code != http.StatusOK || len(saved.Posts) != 2 || saved.Posts[1].DocID != 2 || saved.Posts[1].Type != doctype_file || string(saved.Posts[1].Title) != "Caption &amp; &lt;notes&gt;\nnext line" || len(saved.Posts[1].Files) != 1 {
		t.Fatal("the shared creation helper must preserve ordinary composer text, attachments and append responses")
	}
	file := saved.Posts[1].Files[0]
	filename := filepath.Join(DATA_BASE_PATH, "uploads", "sender", file.Name)
	if original, err := os.ReadFile(filename); err != nil || !bytes.Equal(original, image) || file.OrgName != "photo.png" || file.ThumbnailURL == "" {
		t.Error("composer uploads must keep original bytes and generate their separate thumbnail")
	}
	if _, err := os.Stat(filename+".thumb.png"); err != nil || !strings.Contains(response.Body.String(), `src="`+file.ThumbnailURL+`"`) || strings.Contains(response.Body.String(), `src="`+file.Url+`"`) {
		t.Error("attachment previews must still load only the generated thumbnail")
	}
	before, err := os.ReadFile(filepath.Join(DATA_BASE_PATH, "data", "sender.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(DATA_BASE_PATH, "data", "sender.json.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	uploadsBefore, err := os.ReadDir(filepath.Join(DATA_BASE_PATH, "uploads", "sender"))
	if err != nil {
		t.Fatal(err)
	}
	// A failed profile save after uploading must roll back both original and thumbnail.
	router.POST("/test-failed-composer", func(c *gin.Context) {
		form, err := c.MultipartForm()
		if err != nil {
			t.Fatal(err)
		}
		defer form.RemoveAll()
		if _, _, err := createMessage(c, "sender", "Retry", form.File["files"], queue); err == nil {
			t.Fatal("blocked persistence must fail")
		}
		c.Status(http.StatusInternalServerError)
	})
	request = multipartShareRequest(t, nil, image)
	request.URL.Path = "/test-failed-composer"
	failed := httptest.NewRecorder()
	router.ServeHTTP(failed, request)
	uploadsAfter, err := os.ReadDir(filepath.Join(DATA_BASE_PATH, "uploads", "sender"))
	if err != nil || !reflect.DeepEqual(uploadsBefore, uploadsAfter) {
		t.Error("failed creation must clean up its uploads/thumbnails without touching existing files")
	}
	if after, err := os.ReadFile(filepath.Join(DATA_BASE_PATH, "data", "sender.json")); err != nil || !bytes.Equal(before, after) {
		t.Error("failed creation must leave the previous profile untouched")
	}
}

// The expired-session branch must run before consuming any private shared body.
type unreadShareBody struct{}

func (unreadShareBody) Read([]byte) (int, error) { panic("expired share body was read") }
func (unreadShareBody) Close() error           { return nil }

func TestExpiredShareDoesNotReadBody(t *testing.T) {
	router, _, _ := textSharingTestRouter(t, false)
	request := httptest.NewRequest(http.MethodPost, "/tray/share", nil)
	request.Body = unreadShareBody{}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Error("expired session must fail before parsing or retaining shared data")
	}
}
