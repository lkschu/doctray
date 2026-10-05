package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const maxSharedTextRequestBytes = 256 << 10

// Android often puts a shared URL in text, not url. Keep all distinct non-empty
// fields in order; only identical whole fields are omitted, never substrings.
func sharedMessageText(values map[string][]string) string {
	parts := []string{}
	seen := make(map[string]bool)
	for _, name := range []string{"title", "text", "url"} {
		if len(values[name]) == 0 {
			continue
		}
		value := strings.ReplaceAll(strings.TrimSpace(values[name][0]), "\r", "")
		if value != "" && !seen[value] {
			parts = append(parts, value)
			seen[value] = true
		}
	}
	return strings.Join(parts, "\n")
}

func shareError(c *gin.Context, status int, message string, loginRequired bool) {
	c.HTML(status, "posts/share-error.tmpl", gin.H{"Message": message, "LoginRequired": loginRequired})
}

// Use the existing OIDC session checker, but fail rather than redirect an expired
// POST into login: shared content is not retained or replayed after authentication.
func registerTextSharing(router *gin.Engine, userID func(*gin.Context) (string, error), previews *previewJobQueue) {
	router.POST("/tray/share", func(c *gin.Context) {
		subject, err := userID(c)
		if err != nil || subject == "" {
			shareError(c, http.StatusUnauthorized, "You need to log in before sharing to DocTray. Nothing was saved.", true)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSharedTextRequestBytes)
		form, err := c.MultipartForm()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				shareError(c, http.StatusRequestEntityTooLarge, "The share is too large. Text shares must fit within 256 KiB, including form data.", false)
			} else {
				shareError(c, http.StatusBadRequest, "DocTray could not read this share. Go back and share it again.", false)
			}
			return
		}
		defer form.RemoveAll()
		if len(form.File) != 0 {
			shareError(c, http.StatusBadRequest, "File sharing is not available yet. Nothing was saved.", false)
			return
		}
		text := sharedMessageText(form.Value)
		if text == "" {
			shareError(c, http.StatusBadRequest, "The share contained no text or link. Nothing was saved.", false)
			return
		}
		if _, _, err := createMessage(c, subject, text, nil, previews); err != nil {
			shareError(c, http.StatusInternalServerError, "DocTray could not save this share. Go back and try again.", false)
			return
		}
		c.Redirect(http.StatusSeeOther, "/tray/")
	})
}
