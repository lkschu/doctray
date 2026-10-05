package main

import (
	"errors"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	maxSharedTextBytes       = 256 << 10
	maxSharedAttachmentBytes = 10 << 20
	// Bound the entire request, allowing space for text and multipart headers.
	maxShareRequestBytes = maxSharedAttachmentBytes + maxSharedTextBytes + (64 << 10)
)

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
func registerSharing(router *gin.Engine, userID func(*gin.Context) (string, error), previews *previewJobQueue) {
	router.POST("/tray/share", func(c *gin.Context) {
		subject, err := userID(c)
		if err != nil || subject == "" {
			shareError(c, http.StatusUnauthorized, "You need to log in before sharing to DocTray. Nothing was saved.", true)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxShareRequestBytes)
		form, err := c.MultipartForm()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) || errors.Is(err, multipart.ErrMessageTooLarge) {
				shareError(c, http.StatusRequestEntityTooLarge, "The share is too large or contains too many parts. Try a smaller selection. Nothing was saved.", false)
			} else {
				shareError(c, http.StatusBadRequest, "DocTray could not read this share. Go back and share it again.", false)
			}
			return
		}
		defer form.RemoveAll()
		textBytes := 0
		for _, values := range form.Value {
			for _, value := range values {
				textBytes += len(value)
			}
		}
		if textBytes > maxSharedTextBytes {
			shareError(c, http.StatusRequestEntityTooLarge, "Shared text must total 256 KiB or less. Nothing was saved.", false)
			return
		}
		for name := range form.File {
			if name != "files" {
				shareError(c, http.StatusBadRequest, "DocTray received files in an unexpected form field. Nothing was saved.", false)
				return
			}
		}
		files := form.File["files"]
		var attachmentBytes int64
		for _, file := range files {
			attachmentBytes += file.Size
		}
		if attachmentBytes > maxSharedAttachmentBytes {
			shareError(c, http.StatusRequestEntityTooLarge, "Attachments must total 10 MiB or less. Nothing was saved.", false)
			return
		}
		text := sharedMessageText(form.Value)
		if text == "" && len(files) == 0 {
			shareError(c, http.StatusBadRequest, "The share contained no text, link or file. Nothing was saved.", false)
			return
		}
		if _, _, err := createMessage(c, subject, text, files, previews); err != nil {
			shareError(c, http.StatusInternalServerError, "DocTray could not save this share. Go back and try again.", false)
			return
		}
		c.Redirect(http.StatusSeeOther, "/tray/")
	})
}
