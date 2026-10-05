package main

import (
	"crypto/sha256"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"doctray/internal/previewbuilder"
	"doctray/internal/urlutil"

	"github.com/gin-gonic/gin"
)

// Ignore derived views, previews, tags and stars: their updates must not invalidate
// a text edit. The stored text/date identify the version loaded into the composer.
func (p post) editRevision() string {
	value := fmt.Sprintf("%d\x00%s\x00%s", p.DocID, p.Date, p.Title)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

// Keep previews for unchanged URLs; only new URLs need fresh background jobs.
func reconcilePostPreviews(title string, existing []previewbuilder.URLPreview) []previewbuilder.URLPreview {
	previews := make([]previewbuilder.URLPreview, 0, maxPreviewsPerMessage)
	seen := make(map[string]bool)
	for _, indexes := range find_url_in_string([]byte(title)) {
		url, err := urlutil.NormalizeHTTPURL(title[indexes[0]:indexes[1]])
		if err != nil || seen[url] {
			continue
		}
		seen[url] = true
		var preview previewbuilder.URLPreview
		found := false
		for _, old := range existing {
			if old.URL == url {
				preview = old
				found = true
				break
			}
		}
		if !found {
			preview, err = previewbuilder.PendingURLPreview(url, fmt.Sprintf("%d-%s", time.Now().UnixNano(), rand_seq(8)))
			if err != nil {
				continue
			}
		}
		previews = append(previews, preview)
		if len(previews) == maxPreviewsPerMessage {
			break
		}
	}
	return previews
}

// Register under the authenticated tray group. Reads never persist edit state;
// updates modify only text/previews on the current subject's existing live post.
func registerMessageEditing(routes *gin.RouterGroup, previews *previewJobQueue) {
	routes.GET("/doc-edit/:id", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			c.String(http.StatusBadRequest, "Invalid message ID")
			return
		}
		sub := get_uuid(c)
		withProfileLock(sub, func() {
			profile := get_data(sub)
			message := profile.find_post_by_id(id)
			if message == nil || message.DeletedAt != nil {
				c.String(http.StatusNotFound, "Message is no longer available")
				return
			}
			c.JSON(http.StatusOK, gin.H{"id": message.DocID, "text": html.UnescapeString(string(message.Title)),
				"revision": message.editRevision(), "has_files": len(message.Files) > 0})
		})
	})
	routes.POST("/doc-update", func(c *gin.Context) {
		form, err := c.MultipartForm()
		if err != nil {
			c.String(http.StatusBadRequest, "Invalid edit form")
			return
		}
		if len(form.File) != 0 {
			c.String(http.StatusBadRequest, "Attachments cannot be changed while editing")
			return
		}
		id, err := strconv.Atoi(c.PostForm("id"))
		if err != nil {
			c.String(http.StatusBadRequest, "Invalid message ID")
			return
		}
		title := html.EscapeString(strings.ReplaceAll(strings.TrimSpace(c.PostForm("title")), "\r", ""))
		sub := get_uuid(c)
		withProfileLock(sub, func() {
			profile := get_data(sub)
			message := profile.find_post_by_id(id)
			if message == nil || message.DeletedAt != nil {
				c.String(http.StatusNotFound, "Message is no longer available")
				return
			}
			if c.PostForm("revision") != message.editRevision() {
				c.String(http.StatusConflict, "Message changed; cancel and reopen it before saving")
				return
			}
			if title == "" && len(message.Files) == 0 {
				c.String(http.StatusBadRequest, "A message needs text or an existing attachment")
				return
			}
			message.Title = template.HTML(title)
			message.Webpreview = reconcilePostPreviews(title, message.Webpreview)
			set_data(profile, sub)
			previews.enqueuePendingPreviews(sub, profile)
			c.HTML(http.StatusOK, "base/doc-entry.tmpl", render_post(*message))
		})
	})
}
