package main

import (
	"errors"
	"fmt"
	"html"
	"html/template"
	"mime/multipart"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"doctray/internal/requestlog"
	"doctray/internal/thumbnail"

	"github.com/gin-gonic/gin"
)

// Callers validate content and choose their response: composer fragment or share
// redirect. Creation retains the same lock, uploads, thumbnails and preview jobs.
func createMessage(c *gin.Context, subject, text string, files []*multipart.FileHeader, previews *previewJobQueue) (profile_data, post, error) {
	title := html.EscapeString(strings.ReplaceAll(strings.TrimSpace(text), "\r", ""))
	pendingPreviews := reconcilePostPreviews(title, nil)
	requestlog.FromGin(c).Info("message creation requested", "event", "message.create.requested", "attachment_count", len(files), "preview_url_count", len(pendingPreviews))
	var profile profile_data
	var message post
	var createErr error
	withProfileLock(subject, func() {
		logger := requestlog.FromGin(c).With("component", "thumbnail")
		createdPaths := []string{}
		persisted := false
		defer func() {
			if !persisted {
				for _, filename := range createdPaths {
					removeUpload(filename, logger)
				}
			}
		}()
		profile, createErr = readProfile(subject)
		if createErr != nil {
			return
		}
		docID := get_data_new_id(&profile.Posts)
		now := time.Now().UTC()
		message = post{DocID: docID, Title: template.HTML(title), Type: doctype_mesage, Date: now.Format(http.TimeFormat), Webpreview: pendingPreviews}
		if len(files) > 0 {
			message.Type = doctype_file
			for _, file := range files {
				basename := fmt.Sprintf("%d__%d__%s", docID, now.UnixMilli(), rand_seq(8)) + path.Ext(file.Filename)
				filename := filepath.Join(DATA_BASE_PATH, "uploads", subject, basename)
				createdPaths = append(createdPaths, filename)
				if createErr = c.SaveUploadedFile(file, filename); createErr != nil {
					return
				}
				attachment := docentry_file{Url: "/media/" + basename, OrgName: path.Base(file.Filename), Name: basename}
				thumbnailPath, err := thumbnail.Create(filename)
				if err == nil {
					attachment.ThumbnailURL = "/media/" + filepath.Base(thumbnailPath)
					createdPaths = append(createdPaths, thumbnailPath)
				} else if errors.Is(err, thumbnail.ErrUnsupported) || errors.Is(err, thumbnail.ErrTooLarge) {
					logger.Debug("attachment thumbnail skipped", "event", "thumbnail.skipped", "reason", err.Error())
				} else {
					logger.Warn("attachment thumbnail failed", "event", "thumbnail.failed")
				}
				message.Files = append(message.Files, attachment)
			}
		}
		profile.Posts = append(profile.Posts, message)
		if createErr = writeProfile(profile, subject); createErr != nil {
			return
		}
		persisted = true
		previews.enqueuePendingPreviews(subject, profile)
	})
	if createErr != nil {
		// Filesystem errors can contain private filenames/subjects; do not log them.
		requestlog.FromGin(c).Error("message creation failed", "event", "message.create.failed")
	}
	return profile, message, createErr
}
