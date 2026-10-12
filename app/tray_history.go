package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
)

const trayPageSize = 50

var errTrayHistoryChanged = errors.New("tray history changed")

// The anchor's identity, not its offset, survives additions and local deletion.
// Filter state prevents an old cursor from silently paging a different result.
type trayCursor struct {
	ID      int    `json:"id"`
	Date    string `json:"date"`
	Filters string `json:"filters"`
}

func postMatchesFilters(profile profile_data, message post) bool {
	if profile.Only_favorites && !message.Starred {
		return false
	}
	active := false
	for _, tag := range profile.Tags {
		if tag.Enabled {
			active = true
			for _, id := range message.Tags {
				if id == tag.ID {
					return true
				}
			}
		}
	}
	return !active
}

func trayFilterKey(profile profile_data) string {
	ids := make([]string, 0)
	for _, tag := range profile.Tags {
		if tag.Enabled {
			ids = append(ids, tag.ID)
		}
	}
	sort.Strings(ids)
	value, _ := json.Marshal(struct {
		Starred bool
		Tags    []string
	}{profile.Only_favorites, ids})
	return fmt.Sprintf("%x", sha256.Sum256(value))
}

func encodeTrayCursor(message post, filters string) string {
	value, _ := json.Marshal(trayCursor{ID: message.DocID, Date: message.Date, Filters: filters})
	return base64.RawURLEncoding.EncodeToString(value)
}

func decodeTrayCursor(value string) (trayCursor, error) {
	var cursor trayCursor
	if value == "" || len(value) > 1024 {
		return cursor, errors.New("invalid history cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor, errors.New("invalid history cursor")
	}
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.ID < 0 || len(cursor.Filters) != 64 {
		return cursor, errors.New("invalid history cursor")
	}
	return cursor, nil
}

// Select before preparing display data: only this bounded batch builds tag
// buttons and rendered message HTML. Profile JSON reads still cover the history.
func selectTrayPage(profile profile_data, end int) profile_data {
	selected := make([]post, 0, trayPageSize)
	oldest := -1
	hasOlder := false
	for i := end - 1; i >= 0; i-- {
		message := profile.Posts[i]
		if message.DeletedAt != nil || !postMatchesFilters(profile, message) {
			continue
		}
		if len(selected) == trayPageSize {
			hasOlder = true
			break
		}
		selected = append(selected, message)
		oldest = i
	}
	profile.OlderCursor = ""
	if hasOlder {
		profile.OlderCursor = encodeTrayCursor(profile.Posts[oldest], trayFilterKey(profile))
	}
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	for index := range selected {
		preparePostView(&selected[index], profile.Tags, profile.Tag_map)
		selected[index] = render_post(selected[index])
	}
	profile.Posts = selected
	profile.RemovedPosts = nil
	return profile
}

func initialTrayPage(profile profile_data, includeRemoved bool) profile_data {
	view := selectTrayPage(profile, len(profile.Posts))
	if includeRemoved {
		// Undo survives a filter reset even for a removed card in older history.
		// These carry no message contents in their rendered template.
		for _, message := range profile.Posts {
			if message.DeletedAt != nil {
				view.RemovedPosts = append(view.RemovedPosts, message)
			}
		}
	}
	return view
}

func olderTrayPage(profile profile_data, cursor trayCursor) (profile_data, error) {
	if cursor.Filters != trayFilterKey(profile) {
		return profile_data{}, errTrayHistoryChanged
	}
	for index, message := range profile.Posts {
		if message.DocID == cursor.ID && message.Date == cursor.Date {
			return selectTrayPage(profile, index), nil
		}
	}
	// Another full-page load may have purged the anchor. Never guess an offset.
	return profile_data{}, errTrayHistoryChanged
}

func registerTrayHistory(router *gin.RouterGroup) {
	router.GET("/history", func(c *gin.Context) {
		cursor, err := decodeTrayCursor(c.Query("before"))
		if err != nil {
			c.String(http.StatusBadRequest, "Invalid history cursor. Reload the tray to continue.")
			return
		}
		withProfileLock(get_uuid(c), func() {
			page, err := olderTrayPage(get_data(get_uuid(c)), cursor)
			if err != nil {
				c.String(http.StatusConflict, "History or filters changed. Reload the tray to continue.")
				return
			}
			c.HTML(http.StatusOK, "base/doc-history.tmpl", page)
		})
	})
}
