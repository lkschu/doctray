package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func profileReadTestDirectory(t *testing.T) string {
	t.Helper()
	previous := DATA_BASE_PATH
	DATA_BASE_PATH = t.TempDir()
	t.Cleanup(func() { DATA_BASE_PATH = previous })
	for _, folder := range []string{"data", "uploads/reader"} {
		if err := os.MkdirAll(filepath.Join(DATA_BASE_PATH, folder), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(DATA_BASE_PATH, "data/reader.json")
}

func assertProfileFileUnchanged(t *testing.T, filename string, before os.FileInfo, contents []byte) {
	t.Helper()
	after, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(actual, contents) || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("an unchanged tray load must not rewrite or replace the profile file")
	}
}

func TestTrayLoadSkipsUnchangedWrites(t *testing.T) {
	filename := profileReadTestDirectory(t)
	profile := historyTestProfile(123)
	normalizeProfile(&profile)
	contents, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, contents, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, fullPage := range []bool{false, true, true, false} {
		loaded := loadTrayProfile("reader", fullPage, logger)
		if len(loaded.Posts) != 123 {
			t.Fatal("unchanged reads must retain the complete persisted history")
		}
		assertProfileFileUnchanged(t, filename, before, contents)
	}
	for _, fullPage := range []bool{true, false} {
		loadTrayProfile("absent", fullPage, logger)
		if _, err := os.Stat(filepath.Join(DATA_BASE_PATH, "data/absent.json")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("opening a new empty tray must not create a profile until a mutation")
		}
	}
}

func TestProfileNormalizationPersistsOnlyChanges(t *testing.T) {
	filename := profileReadTestDirectory(t)
	contents := []byte(`{"tags":[{"id":"keep","name":"First"},{"id":"keep","name":"Duplicate"},{"id":"other","name":"Other"},{"id":"other","name":"Duplicate other"}],"posts":[{"id":1,"title":"PRIVATE NOTE","type":"legacy","tags":["keep","unknown",""],"files":[{"orgname":"PRIVATE.pdf","url":"/media/PRIVATE.pdf"}]},{"id":2,"type":"msg","tags":null}]}`)
	if err := os.WriteFile(filename, contents, 0600); err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&diagnostics, &slog.HandlerOptions{Level: slog.LevelDebug}))
	loaded, changed, err := readProfileWithChanges("reader", logger)
	if err != nil || !changed || len(loaded.Tags) != 2 || loaded.Tags[0].Name != "First" || loaded.Tags[1].ID != "other" || loaded.Tags[1].Nr != "1" {
		t.Fatal("normalization must retain the first tag per ID and report stored changes")
	}
	if loaded.Tag_map["keep"] != &loaded.Tags[0] || loaded.Tag_map["other"] != &loaded.Tags[1] {
		t.Fatal("tag lookup pointers must target the retained tags after compaction")
	}
	message := loaded.Posts[0]
	if message.Type != doctype_mesage || !reflect.DeepEqual(message.Tags, []string{"keep"}) || message.Files[0].Icon != known_file_suffixes[".pdf"] || message.Files[0].ThumbnailURL != "" || loaded.Posts[1].Tags == nil {
		t.Fatal("stored normalization must preserve type/icon/tag defaults without inventing thumbnails")
	}
	if after, err := os.ReadFile(filename); err != nil || !bytes.Equal(after, contents) {
		t.Fatal("ordinary profile reads must normalize in memory without writing")
	}
	for _, message := range loaded.Posts {
		if len(message.Tags_enabled) != 0 {
			t.Fatal("normalization must not allocate per-card tag view data")
		}
	}
	for _, private := range []string{"PRIVATE", "reader", "keep", "/media/"} {
		if strings.Contains(diagnostics.String(), private) {
			t.Fatal("profile timings must not log private content, identifiers or paths")
		}
	}
	for _, field := range []string{`"event":"persistence.read.completed"`, `"read_us":`, `"decode_us":`, `"normalize_us":`, `"post_count":2`} {
		if !strings.Contains(diagnostics.String(), field) {
			t.Errorf("missing safe timing field %s", field)
		}
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(diagnostics.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"time": true, "level": true, "msg": true, "event": true,
		"read_us": true, "decode_us": true, "normalize_us": true, "post_count": true}
	for key := range fields {
		if !allowed[key] {
			t.Errorf("unexpected diagnostic field %q", key)
		}
	}
	for _, key := range []string{"read_us", "decode_us", "normalize_us", "post_count"} {
		if value, ok := fields[key].(float64); !ok || value < 0 {
			t.Errorf("diagnostic %s must be a nonnegative number", key)
		}
	}
	loadTrayProfile("reader", false, logger)
	if _, changed, err := readProfileWithChanges("reader", nil); err != nil || changed {
		t.Fatal("a tray load must persist normalization once, with no changes on reread")
	}
	before, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	contents, err = os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	loadTrayProfile("reader", true, logger)
	assertProfileFileUnchanged(t, filename, before, contents)
}

func TestStoredNormalizationChangeFlags(t *testing.T) {
	tagMap := map[string]*tag{"keep": {ID: "keep"}}
	for _, test := range []struct {
		name    string
		message post
		changed bool
	}{
		{name: "clean", message: post{Type: doctype_mesage, Tags: []string{"keep"}}},
		{name: "type", message: post{Type: "legacy", Tags: []string{}}, changed: true},
		{name: "icon", message: post{Type: doctype_file, Tags: []string{}, Files: []docentry_file{{OrgName: "notes.pdf"}}}, changed: true},
		{name: "tag IDs", message: post{Type: doctype_mesage, Tags: []string{"keep", "unknown", ""}}, changed: true},
		{name: "null tags", message: post{Type: doctype_mesage}, changed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if changed := normalizeStoredPost(&test.message, tagMap); changed != test.changed {
				t.Fatalf("changed = %t, want %t", changed, test.changed)
			}
			if normalizeStoredPost(&test.message, tagMap) {
				t.Fatal("each normalization must become a no-op after its first application")
			}
		})
	}
	profile := profile_data{Tags: []tag{{ID: "keep"}, {ID: "keep"}}}
	if !normalizeProfile(&profile) || normalizeProfile(&profile) {
		t.Fatal("duplicate tag compaction alone must require exactly one save")
	}
}

func TestLazyPostViews(t *testing.T) {
	profileReadTestDirectory(t)
	profile := historyTestProfile(123)
	normalizeProfile(&profile)
	set_data(profile, "reader")
	loaded := get_data("reader")
	view := initialTrayPage(loaded, false)
	if len(view.Posts) != trayPageSize {
		t.Fatal("initial tray preparation must stay bounded")
	}
	for i := range view.Posts {
		message := &view.Posts[i]
		if len(message.Tags_enabled) != len(view.Tags) {
			t.Fatal("selected cards must rebuild all tag controls")
		}
		for index, segment := range message.Tags_enabled {
			if segment.BackRef != message || segment.Tag != &view.Tags[index] {
				t.Fatal("derived tag pointers must refer to the final rendered card and profile tag")
			}
		}
	}
	for _, message := range loaded.Posts {
		if message.Tags_enabled != nil {
			t.Fatal("preparing a page must leave every stored/unselected card view-free")
		}
	}
	// The tag-toggle handler prepares just its target before using this method.
	message := &loaded.Posts[4]
	preparePostView(message, loaded.Tags, loaded.Tag_map)
	if err := message.toggle_tag_by_id("even"); err != nil || len(message.Tags) != 0 || message.Tags_enabled[0].Enabled {
		t.Fatal("tag removal must still work after lazy preparation")
	}
	if err := message.toggle_tag_by_id("even"); err != nil || !reflect.DeepEqual(message.Tags, []string{"even"}) || !message.Tags_enabled[0].Enabled {
		t.Fatal("tag addition must still work after lazy preparation")
	}
	if err := message.toggle_tag_by_id("unknown"); err == nil {
		t.Fatal("unknown tag IDs must still be rejected")
	}
}

func TestFailedCleanupSkipsProfileRewrite(t *testing.T) {
	filename := profileReadTestDirectory(t)
	directory := filepath.Join(DATA_BASE_PATH, "uploads/reader")
	original := filepath.Join(directory, "blocked.bin")
	blockedThumbnail := original + ".thumb.png"
	if err := os.WriteFile(original, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blockedThumbnail, 0700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(blockedThumbnail, "blocker")
	if err := os.WriteFile(blocker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	profile := historyTestProfile(1)
	profile.Posts[0].MarkDeleted(time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC))
	profile.Posts[0].Files = []docentry_file{{Url: "/media/blocked.bin", OrgName: "blocked.bin"}}
	normalizeProfile(&profile)
	set_data(profile, "reader")
	before, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	loaded := loadTrayProfile("reader", true, logger)
	if len(loaded.Posts) != 1 || loaded.Posts[0].DeletedAt == nil {
		t.Fatal("failed cleanup must retain the marked record for retry")
	}
	assertProfileFileUnchanged(t, filename, before, contents)
	if _, err := os.Stat(original); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("this failed cleanup must already have removed the original attachment")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if loaded := loadTrayProfile("reader", true, logger); len(loaded.Posts) != 0 {
		t.Fatal("a later load must retry partial cleanup and persist successful removal")
	}
	if saved := get_data("reader"); len(saved.Posts) != 0 {
		t.Fatal("successful cleanup must be saved")
	}
}

func TestShareRedirectNeedsNoNormalizationWrite(t *testing.T) {
	for _, withFile := range []bool{false, true} {
		name := "text"
		if withFile {
			name = "attachment"
		}
		t.Run(name, func(t *testing.T) {
			router, _, _ := sharingTestRouter(t, true)
			var files []shareTestFile
			if withFile {
				files = append(files, shareTestFile{name: "notes.txt", data: []byte("plain text")})
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, multipartFileShareRequest(t, map[string]string{"text": "Saved"}, files...))
			if response.Code != http.StatusSeeOther {
				t.Fatal("the share must save and redirect normally")
			}
			if _, changed, err := readProfileWithChanges("sender", nil); err != nil || changed {
				t.Fatal("new text and attachment messages must already have normalized stored defaults")
			}
			filename := filepath.Join(DATA_BASE_PATH, "data/sender.json")
			before, err := os.Stat(filename)
			if err != nil {
				t.Fatal(err)
			}
			contents, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			loadTrayProfile("sender", true, slog.New(slog.NewJSONHandler(io.Discard, nil)))
			assertProfileFileUnchanged(t, filename, before, contents)
		})
	}
}
