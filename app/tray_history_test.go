package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func historyTestProfile(count int) profile_data {
	profile := profile_data{Tags: []tag{{ID: "even"}, {ID: "third"}}}
	profile.Tag_map = map[string]*tag{"even": &profile.Tags[0], "third": &profile.Tags[1]}
	for id := 0; id < count; id++ {
		message := post{DocID: id, Date: "Tue, 03 Oct 2000 14:05:59 GMT", Title: template.HTML(fmt.Sprintf("**Message %d**", id)), Starred: id%5 == 0}
		if id%2 == 0 {
			message.Tags = append(message.Tags, "even")
		}
		if id%3 == 0 {
			message.Tags = append(message.Tags, "third")
		}
		profile.Posts = append(profile.Posts, message)
	}
	return profile
}

func historyPostIDs(posts []post) []int {
	ids := make([]int, 0, len(posts))
	for _, message := range posts {
		ids = append(ids, message.DocID)
	}
	return ids
}

func TestTrayHistoryPages(t *testing.T) {
	for _, count := range []int{0, 1, 49, 50, 51, 100, 123, 400} {
		for _, filters := range []string{"none", "tags", "starred", "both"} {
			t.Run(fmt.Sprintf("%d/%s", count, filters), func(t *testing.T) {
				profile := historyTestProfile(count)
				if filters == "tags" || filters == "both" {
					profile.Tags[0].Enabled, profile.Tags[1].Enabled = true, true
				}
				profile.Only_favorites = filters == "starred" || filters == "both"
				want := make([]int, 0)
				for id := 0; id < count; id++ {
					if profile.Only_favorites && id%5 != 0 {
						continue
					}
					if (filters == "tags" || filters == "both") && id%2 != 0 && id%3 != 0 {
						continue
					}
					want = append(want, id)
				}
				view := initialTrayPage(profile, false)
				all := make([]int, 0)
				for page := 0; ; page++ {
					if page > count/trayPageSize+1 || len(view.Posts) > trayPageSize {
						t.Fatal("pagination must advance in bounded batches")
					}
					remaining := len(want) - len(all)
					start := remaining - trayPageSize
					if start < 0 {
						start = 0
					}
					ids := historyPostIDs(view.Posts)
					if !reflect.DeepEqual(ids, want[start:remaining]) {
						t.Fatalf("page = %v, want %v", ids, want[start:remaining])
					}
					for _, message := range view.Posts {
						if message.Title != render_post(profile.Posts[message.DocID]).Title {
							t.Fatal("each selected card must be formatted exactly once")
						}
					}
					all = append(ids, all...)
					if view.OlderCursor == "" {
						break
					}
					cursor, err := decodeTrayCursor(view.OlderCursor)
					if err != nil {
						t.Fatal(err)
					}
					view, err = olderTrayPage(profile, cursor)
					if err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(all, want) {
					t.Fatal("pages must cover the filtered history without gaps or duplicates")
				}
				for id, message := range profile.Posts {
					if string(message.Title) != fmt.Sprintf("**Message %d**", id) {
						t.Fatal("paging must not mutate any persisted source titles")
					}
				}
			})
		}
	}
}

func TestTrayHistoryCursorAndUndo(t *testing.T) {
	profile := historyTestProfile(123)
	profile.Posts[1].DeletedAt = new(time.Time)
	profile.Posts[120].DeletedAt = new(time.Time)
	view := initialTrayPage(profile, true)
	if len(view.Posts) != 50 || !reflect.DeepEqual(historyPostIDs(view.RemovedPosts), []int{1, 120}) || view.Posts[0].DocID != 72 {
		t.Fatal("Undo rows must survive refresh without consuming the 50 matching live cards")
	}
	if len(initialTrayPage(profile, false).RemovedPosts) != 0 {
		t.Fatal("full tray loads must not resurrect Undo")
	}
	cursor, err := decodeTrayCursor(view.OlderCursor)
	if err != nil {
		t.Fatal(err)
	}
	profile.Posts = append(profile.Posts, post{DocID: 123, Date: "later", Title: "New"})
	profile.Posts[72].DeletedAt = new(time.Time) // The cursor anchor remains valid.
	page, err := olderTrayPage(profile, cursor)
	if err != nil || len(page.Posts) != 50 || page.Posts[0].DocID != 22 || page.Posts[49].DocID != 71 || len(page.RemovedPosts) != 0 {
		t.Fatal("new messages or marked anchors must not shift or duplicate older pages")
	}
	profile.Tags[0].Enabled = true
	if _, err := olderTrayPage(profile, cursor); !errors.Is(err, errTrayHistoryChanged) {
		t.Fatal("an old cursor must not page changed filters")
	}
	profile.Tags[0].Enabled = false
	profile.Posts[72].Date = "different identity"
	if _, err := olderTrayPage(profile, cursor); !errors.Is(err, errTrayHistoryChanged) {
		t.Fatal("a reused ID must not accept the old anchor")
	}
	profile.Posts = append(profile.Posts[:72], profile.Posts[73:]...)
	if _, err := olderTrayPage(profile, cursor); !errors.Is(err, errTrayHistoryChanged) {
		t.Fatal("a missing anchor must request a refresh, never guess an offset")
	}
	for _, value := range []string{"", "!", "e30", strings.Repeat("a", 1025), encodeTrayCursor(post{DocID: -1}, strings.Repeat("a", 64))} {
		if _, err := decodeTrayCursor(value); err == nil {
			t.Errorf("invalid cursor accepted: %q", value)
		}
	}
	profile.Tags[0].Enabled, profile.Tags[1].Enabled = true, true
	key := trayFilterKey(profile)
	profile.Tags[0], profile.Tags[1] = profile.Tags[1], profile.Tags[0]
	if trayFilterKey(profile) != key {
		t.Fatal("tag reordering must not invalidate an unchanged filter selection")
	}
}

func TestTrayHistoryRouteAndFragments(t *testing.T) {
	router, _ := messageEditTestRouter(t)
	tmpl, err := template.ParseFiles("templates/base/doc.tmpl", "templates/base/doc-list.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	router.SetHTMLTemplate(tmpl)
	registerTrayHistory(router.Group("/tray"))
	profile := historyTestProfile(101)
	profile.Posts[0].DeletedAt = new(time.Time)
	set_data(profile, "editor")
	filename := filepath.Join(DATA_BASE_PATH, "data/editor.json")
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	view := initialTrayPage(get_data("editor"), true)
	var initial bytes.Buffer
	if err := tmpl.ExecuteTemplate(&initial, "base/doc-list.tmpl", view); err != nil {
		t.Fatal(err)
	}
	if strings.Count(initial.String(), `class="doc-entry-container"`) != 51 || !strings.Contains(initial.String(), `id="doc-undo-list"`) {
		t.Fatal("initial list must include 50 matching cards and a separate retained Undo group")
	}
	if !strings.Contains(initial.String(), `data-doc-id="0"`) || !strings.Contains(initial.String(), `data-doc-id="51"`) {
		t.Fatal("live and retained Undo wrappers need stable positional IDs for restored-card placement")
	}
	if !strings.Contains(initial.String(), `hx-sync="#tray-container:drop"`) || !strings.Contains(initial.String(), `hx-target="#doc-history-control"`) || strings.Contains(initial.String(), `hx-trigger="load"`) {
		t.Fatal("older history must be explicit and use the shared request lock")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tray/history?before="+url.QueryEscape(view.OlderCursor), nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || strings.Count(body, `class="doc-entry-container"`) != 50 || !strings.Contains(body, `id="doc-entry-container-1"`) || !strings.Contains(body, `id="doc-entry-container-50"`) || strings.Contains(body, `id="doc-entry-container-51"`) || strings.Contains(body, "Removed") || strings.Contains(body, "Load older messages") {
		t.Fatal("the last older batch must return only matching cards and an exhausted control")
	}
	if strings.Contains(body, `id="doc-container"`) || strings.Contains(body, `id="workspace-container"`) || strings.Contains(body, `id="form"`) {
		t.Fatal("history fragments must not replace the workspace, composer or scroll container")
	}
	if after, err := os.ReadFile(filename); err != nil || !bytes.Equal(before, after) {
		t.Fatal("loading history must not save, purge, or end Undo")
	}
	profile.Only_favorites = true
	set_data(profile, "editor")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tray/history?before="+url.QueryEscape(view.OlderCursor), nil))
	if response.Code != http.StatusConflict {
		t.Fatal("filter changes in another tab must reject stale paging")
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/tray/history?before=invalid", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatal("malformed paging requests must be rejected")
	}
}
