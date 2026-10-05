package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

	"doctray/internal/openidauth"
	"doctray/internal/requestlog"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func sharingTestRouter(t *testing.T, loggedIn bool) (*gin.Engine, *previewJobQueue, *bytes.Buffer) {
	t.Helper()
	return sharingTestRouterWithUserID(t, func(c *gin.Context) (string, error) {
		if !loggedIn {
			return "", openidauth.ErrSessionExpired
		}
		return "sender", nil
	})
}

func sharingTestRouterWithUserID(t *testing.T, userID func(*gin.Context) (string, error)) (*gin.Engine, *previewJobQueue, *bytes.Buffer) {
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
	router.MaxMultipartMemory = 10 << 20
	tmpl, err := template.ParseGlob("templates/*/*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	router.SetHTMLTemplate(tmpl)
	logs := new(bytes.Buffer)
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	router.Use(requestlog.Middleware(logger))
	store := newSessionStore([]byte("01234567890123456789012345678901"), []byte("01234567890123456789012345678901"), true)
	router.Use(sessions.Sessions("session", store))
	queue := &previewJobQueue{jobs: make(chan previewJob, previewJobQueueSize), activeJobs: make(map[string]bool), logger: logger}
	registerSharing(router, sharingSessionUserID(store, userID), queue)
	return router, queue, logs
}

func multipartShareRequest(t *testing.T, fields map[string]string, file []byte) *http.Request {
	t.Helper()
	if file == nil {
		return multipartFileShareRequest(t, fields)
	}
	return multipartFileShareRequest(t, fields, shareTestFile{name: "photo.png", data: file})
}

type shareTestFile struct {
	name  string
	data  []byte
	field string
}

func multipartFileShareRequest(t *testing.T, fields map[string]string, files ...shareTestFile) *http.Request {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := form.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range files {
		field := file.field
		if field == "" {
			field = "files"
		}
		part, err := form.CreateFormFile(field, file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.data); err != nil {
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
	router, queue, logs := sharingTestRouter(t, true)
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
		{name: "expired login with file", fields: map[string]string{"text": "Private rejected text"}, file: []byte("private file bytes"), status: http.StatusUnauthorized},
		{name: "empty", loggedIn: true, fields: map[string]string{"text": " \r\n "}, status: http.StatusBadRequest},
		{name: "unknown fields", loggedIn: true, fields: map[string]string{"subject": "not a message"}, status: http.StatusBadRequest},
		{name: "malformed multipart", loggedIn: true, malformed: true, status: http.StatusBadRequest},
		{name: "oversized", loggedIn: true, fields: map[string]string{"text": strings.Repeat("x", maxSharedTextBytes+1)}, status: http.StatusRequestEntityTooLarge},
		{name: "failed persistence", loggedIn: true, fields: map[string]string{"text": "Private rejected text"}, blockWrite: true, status: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, queue, logs := sharingTestRouter(t, test.loggedIn)
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
	router, queue, _ := sharingTestRouter(t, true)
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
	router, _, _ := sharingTestRouter(t, false)
	request := httptest.NewRequest(http.MethodPost, "/tray/share", nil)
	request.Body = unreadShareBody{}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Error("expired session must fail before parsing or retaining shared data")
	}
}

func TestShareAuthenticationDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name       string
		cookie     bool
		broken     bool
		authError  error
		fetchSite  string
		reason     string
		loggedSite string
	}{
		{name: "missing cookie", authError: openidauth.ErrInvalidSession, fetchSite: "cross-site", reason: "missing_cookie", loggedSite: "cross-site"},
		{name: "expired session", cookie: true, authError: fmt.Errorf("private error token: %w", openidauth.ErrSessionExpired), fetchSite: "same-origin", reason: "expired_session", loggedSite: "same-origin"},
		{name: "invalid authenticated data", cookie: true, authError: openidauth.ErrInvalidSession, reason: "invalid_session", loggedSite: "unknown"},
		{name: "undecodable cookie", cookie: true, broken: true, reason: "invalid_session", loggedSite: "unknown"},
		{name: "private header and error", cookie: true, authError: errors.New("private error token"), fetchSite: "private header token", reason: "invalid_session", loggedSite: "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			router, queue, logs := sharingTestRouterWithUserID(t, func(c *gin.Context) (string, error) {
				called = true
				return "", test.authError
			})
			router.GET("/test-session", func(c *gin.Context) {
				session := sessions.Default(c)
				session.Set("auth_login_binding", "private binding token")
				if err := session.Save(); err != nil {
					t.Fatal(err)
				}
			})
			request := httptest.NewRequest(http.MethodPost, "/tray/share?private-query-token", nil)
			request.Body = unreadShareBody{}
			request.Header.Set("Sec-Fetch-Site", test.fetchSite)
			request.Header.Set("Authorization", "Bearer private authorization token")
			request.Header.Set("Referer", "https://private.example/source")
			cookieValue := ""
			if test.cookie {
				if test.broken {
					cookieValue = "private broken cookie token"
					request.AddCookie(&http.Cookie{Name: "session", Value: cookieValue})
				} else {
					seed := httptest.NewRecorder()
					router.ServeHTTP(seed, httptest.NewRequest(http.MethodGet, "/test-session", nil))
					cookies := seed.Result().Cookies()
					if len(cookies) != 1 {
						t.Fatal("diagnostic fixture must produce one cookie")
					}
					cookieValue = cookies[0].Value
					request.AddCookie(cookies[0])
				}
			}
			logs.Reset()
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Location") != "" || !strings.Contains(response.Body.String(), `href="/login"`) {
				t.Fatal("all authentication rejections must fail safely before reading the body")
			}
			if called == test.broken {
				t.Error("an undecodable cookie must be rejected before calling Gin's session adapter")
			}
			count := 0
			for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
				var entry map[string]any
				if err := json.Unmarshal([]byte(line), &entry); err != nil {
					t.Fatal(err)
				}
				if entry["event"] == "share.auth.rejected" {
					count++
					if entry["reason"] != test.reason || entry["fetch_site"] != test.loggedSite || entry["level"] != "WARN" {
						t.Error("authentication diagnostics must contain only the expected stable categories")
					}
				}
			}
			if count != 1 || len(queue.jobs) != 0 {
				t.Error("rejected shares must log one diagnostic and queue no preview work")
			}
			for _, private := range []string{"private error token", "private header token", "private binding token", "private authorization token", "private.example", "private-query-token", cookieValue} {
				if private != "" && (strings.Contains(logs.String(), private) || strings.Contains(response.Body.String(), private)) {
					t.Error("diagnostics and failure pages must not disclose cookies, errors or private headers")
				}
			}
			if _, err := os.Stat(filepath.Join(DATA_BASE_PATH, "data", "sender.json")); !os.IsNotExist(err) {
				t.Error("authentication rejection must not create a profile")
			}
		})
	}
}

func TestFileShareSave(t *testing.T) {
	image, err := os.ReadFile("resources/tray_192.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		fields     map[string]string
		files      []shareTestFile
		title      string
		thumbnails int
		previews   int
	}{
		{name: "image only", files: []shareTestFile{{name: "private-photo.png", data: image}}, thumbnails: 1},
		{name: "multiple images with text", fields: map[string]string{"text": "Private caption & <notes> https://example.com/", "subject": "other"},
			files: []shareTestFile{{name: "private-first.png", data: image}, {name: "private-second.png", data: image}},
			title: "Private caption &amp; &lt;notes&gt; https://example.com/", thumbnails: 2, previews: 1},
		{name: "PDF icon fallback", files: []shareTestFile{{name: "private-paper.pdf", data: []byte("%PDF-1.4\nfixture bytes")}}},
		{name: "unsupported format", files: []shareTestFile{{name: "private-data.unknown", data: []byte("private binary contents")}}},
		{name: "failed thumbnail", files: []shareTestFile{{name: "private-broken.png", data: []byte("\x89PNG\r\n\x1a\n")}}},
		{name: "empty file", files: []shareTestFile{{name: "private-empty.txt"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, queue, logs := sharingTestRouter(t, true)
			set_data(profile_data{Posts: []post{{DocID: 1, Title: "Existing message"}}}, "sender")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, multipartFileShareRequest(t, test.fields, test.files...))
			if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/tray/" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("file share must save and redirect once: status %d", response.Code)
			}
			saved := get_data("sender")
			if len(saved.Posts) != 2 || saved.Posts[0].Title != "Existing message" {
				t.Fatal("file sharing must append one message without replacing existing messages")
			}
			message := saved.Posts[1]
			if message.DocID != 2 || message.Type != doctype_file || string(message.Title) != test.title || len(message.Files) != len(test.files) || message.Starred || len(message.Tags) != 0 || len(message.Webpreview) != test.previews || len(queue.jobs) != test.previews {
				t.Fatal("files and optional text must use normal message metadata and preview behavior")
			}
			rendered := httptest.NewRecorder()
			if err := router.HTMLRender.Instance("base/doc-entry.tmpl", render_post(message)).Render(rendered); err != nil {
				t.Fatal(err)
			}
			thumbnailCount := 0
			for index, file := range message.Files {
				original, err := os.ReadFile(filepath.Join(DATA_BASE_PATH, "uploads", "sender", file.Name))
				if err != nil || !bytes.Equal(original, test.files[index].data) || file.OrgName != test.files[index].name || file.Icon == "" {
					t.Error("shared files must retain original bytes/names and an icon in the current user's upload directory")
				}
				if file.ThumbnailURL != "" {
					thumbnailCount++
					if _, err := os.Stat(filepath.Join(DATA_BASE_PATH, "uploads", "sender", file.Name)+".thumb.png"); err != nil || !strings.Contains(rendered.Body.String(), `src="`+file.ThumbnailURL+`"`) {
						t.Error("image previews must use an existing generated thumbnail")
					}
				}
				if strings.Contains(rendered.Body.String(), `src="`+file.Url+`"`) || strings.Contains(logs.String(), file.OrgName) {
					t.Error("file previews/logs must not load originals or expose private filenames")
				}
			}
			if thumbnailCount != test.thumbnails {
				t.Errorf("generated thumbnails = %d, want %d", thumbnailCount, test.thumbnails)
			}
			if strings.Contains(logs.String(), "Private caption") || strings.Contains(logs.String(), "private binary contents") || strings.Contains(logs.String(), "https://example.com/") || strings.Contains(logs.String(), "sender") {
				t.Error("DEBUG logs must not contain private captions, file bytes, URLs or subjects")
			}
			if _, err := os.Stat(filepath.Join(DATA_BASE_PATH, "uploads", "other")); !os.IsNotExist(err) {
				t.Error("a submitted subject field must never choose the upload account")
			}
		})
	}
}

func TestFileShareLimitsAndFailures(t *testing.T) {
	for _, test := range []struct {
		name          string
		fields        map[string]string
		sizes         []int
		fileField     string
		unknownLength bool
		blockWrite    bool
		status        int
	}{
		{name: "exact single-file limit", sizes: []int{maxSharedAttachmentBytes}, status: http.StatusSeeOther},
		{name: "exact combined-file limit", sizes: []int{maxSharedAttachmentBytes / 2, maxSharedAttachmentBytes / 2}, status: http.StatusSeeOther},
		{name: "single-file too large", sizes: []int{maxSharedAttachmentBytes + 1}, status: http.StatusRequestEntityTooLarge},
		{name: "combined files too large", sizes: []int{maxSharedAttachmentBytes / 2, maxSharedAttachmentBytes/2 + 1}, status: http.StatusRequestEntityTooLarge},
		{name: "whole request too large without content length", sizes: []int{maxShareRequestBytes + 1}, unknownLength: true, status: http.StatusRequestEntityTooLarge},
		{name: "exact text limit", fields: map[string]string{"text": strings.Repeat("x", maxSharedTextBytes)}, status: http.StatusSeeOther},
		{name: "combined text too large", fields: map[string]string{"title": strings.Repeat("x", maxSharedTextBytes/2), "text": strings.Repeat("x", maxSharedTextBytes/2+1)}, status: http.StatusRequestEntityTooLarge},
		{name: "unexpected file field", sizes: []int{16}, fileField: "unexpected", status: http.StatusBadRequest},
		{name: "failed file persistence", sizes: []int{16, 16}, fields: map[string]string{"text": "https://example.com/"}, blockWrite: true, status: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			router, queue, logs := sharingTestRouter(t, true)
			// Force multipart disk staging and check cleanup on success and rejection.
			router.MaxMultipartMemory = 1 << 10
			temporaryUploads := t.TempDir()
			t.Setenv("TMPDIR", temporaryUploads)
			set_data(profile_data{Posts: []post{{DocID: 1, Title: "Existing message"}}}, "sender")
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
			files := []shareTestFile{}
			for index, size := range test.sizes {
				files = append(files, shareTestFile{name: fmt.Sprintf("private-file-%d.bin", index), data: bytes.Repeat([]byte{'x'}, size), field: test.fileField})
			}
			request := multipartFileShareRequest(t, test.fields, files...)
			if test.unknownLength {
				request.ContentLength = -1
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("share status = %d, want %d with no-store", response.Code, test.status)
			}
			uploads, err := os.ReadDir(filepath.Join(DATA_BASE_PATH, "uploads", "sender"))
			if err != nil {
				t.Fatal(err)
			}
			if test.status == http.StatusSeeOther {
				if len(get_data("sender").Posts) != 2 || len(uploads) != len(files) || response.Header().Get("Location") != "/tray/" {
					t.Error("boundary-sized shares must save one message and all originals before redirecting")
				}
			} else {
				if after, err := os.ReadFile(filename); err != nil || !bytes.Equal(before, after) || len(uploads) != 0 || len(queue.jobs) != 0 {
					t.Error("rejected/failed file shares must leave the profile intact, roll back uploads and queue no previews")
				}
				if response.Header().Get("Location") != "" || !strings.Contains(response.Body.String(), "Share not saved") {
					t.Error("failed shares must not report success or redirect")
				}
			}
			if entries, err := os.ReadDir(temporaryUploads); err != nil || len(entries) != 0 {
				t.Error("multipart temporary uploads must be removed, including oversized requests")
			}
			if strings.Contains(logs.String(), "private-file-") || strings.Contains(logs.String(), "sender.json") || strings.Contains(logs.String(), "https://example.com/") {
				t.Error("file-share failures/logs must not reveal private filenames, subjects or URLs")
			}
		})
	}
}
