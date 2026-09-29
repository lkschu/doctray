// Package thumbnail creates bounded raster previews without modifying uploads.
package thumbnail

import (
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

const (
	maxSide        = 320
	maxInputSide   = 8192
	maxInputPixels = 16_000_000
	maxInputBytes  = 32 * 1024 * 1024
)

var (
	ErrUnsupported = errors.New("unsupported thumbnail format")
	ErrTooLarge    = errors.New("image exceeds thumbnail limits")
	// Uploads are synchronous, but different profiles can upload concurrently.
	processingSlots = make(chan struct{}, 2)
)

func Path(originalPath string) string {
	return originalPath + ".thumb.png"
}

func allowedDimensions(width, height int) bool {
	return width > 0 && height > 0 &&
		width <= maxInputSide && height <= maxInputSide &&
		int64(width)*int64(height) <= maxInputPixels
}

// Create writes a separate PNG thumbnail, returning its path only on success.
// It checks byte size, format, and dimensions before fully decoding an image.
func Create(originalPath string) (string, error) {
	file, err := os.Open(originalPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > maxInputBytes {
		return "", ErrTooLarge
	}
	config, format, err := image.DecodeConfig(io.LimitReader(file, maxInputBytes))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return "", ErrUnsupported
		}
		return "", err
	}
	switch format {
	case "jpeg", "png", "gif", "webp":
	default:
		return "", ErrUnsupported
	}
	if !allowedDimensions(config.Width, config.Height) {
		return "", ErrTooLarge
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	processingSlots <- struct{}{}
	defer func() { <-processingSlots }()
	img, err := imaging.Decode(io.LimitReader(file, maxInputBytes), imaging.AutoOrientation(true))
	if err != nil {
		return "", err
	}
	if !allowedDimensions(img.Bounds().Dx(), img.Bounds().Dy()) {
		return "", ErrTooLarge
	}
	// Fit preserves aspect ratio and does not upscale images smaller than the box.
	img = imaging.Fit(img, maxSide, maxSide, imaging.Lanczos)

	output, err := os.CreateTemp(filepath.Dir(originalPath), ".thumbnail-*.png")
	if err != nil {
		return "", err
	}
	tempPath := output.Name()
	defer os.Remove(tempPath)
	encodeErr := imaging.Encode(output, img, imaging.PNG)
	closeErr := output.Close()
	if encodeErr != nil {
		return "", encodeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	thumbnailPath := Path(originalPath)
	if err := os.Rename(tempPath, thumbnailPath); err != nil {
		return "", err
	}
	return thumbnailPath, nil
}
