package thumbnail

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestCreate(t *testing.T) {
	for _, test := range []struct {
		name          string
		width, height int
		want          image.Point
	}{
		{"landscape", 640, 320, image.Pt(320, 160)},
		{"portrait", 320, 640, image.Pt(160, 320)},
		{"small", 7, 4, image.Pt(7, 4)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var original bytes.Buffer
			img := image.NewNRGBA(image.Rect(0, 0, test.width, test.height))
			img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
			if err := png.Encode(&original, img); err != nil {
				t.Fatal(err)
			}
			// Detection is based on file contents, not a filename extension.
			filename := filepath.Join(t.TempDir(), "upload.bin")
			if err := os.WriteFile(filename, original.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			thumbnailPath, err := Create(filename)
			if err != nil {
				t.Fatal(err)
			}
			if thumbnailPath != Path(filename) {
				t.Fatalf("thumbnail path = %q, want %q", thumbnailPath, Path(filename))
			}
			data, err := os.ReadFile(thumbnailPath)
			if err != nil {
				t.Fatal(err)
			}
			thumb, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if got := thumb.Bounds().Size(); got != test.want {
				t.Errorf("thumbnail size = %v, want %v", got, test.want)
			}
			savedOriginal, err := os.ReadFile(filename)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(savedOriginal, original.Bytes()) {
				t.Error("original upload was modified")
			}
			if test.name == "small" {
				_, _, _, alpha := thumb.At(0, 0).RGBA()
				if alpha != 128*257 {
					t.Errorf("alpha = %d, want preserved transparency", alpha)
				}
			}
		})
	}
}

func TestCreateJPEGOrientation(t *testing.T) {
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 8, 4)), nil); err != nil {
		t.Fatal(err)
	}
	// Insert a little-endian EXIF orientation=6 (90 degrees clockwise) APP1.
	exif := []byte{
		'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0,
		1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0,
	}
	data := append([]byte{}, encoded.Bytes()[:2]...)
	data = append(data, 0xff, 0xe1, 0, byte(len(exif)+2))
	data = append(data, exif...)
	data = append(data, encoded.Bytes()[2:]...)
	filename := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	thumbnailData, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(thumbnailData))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 4 || config.Height != 8 {
		t.Errorf("oriented dimensions = %dx%d, want 4x8", config.Width, config.Height)
	}
}

func TestCreateGIFUsesFirstFrame(t *testing.T) {
	palette := color.Palette{color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	first := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	second := image.NewPaletted(first.Bounds(), palette)
	for i := range second.Pix {
		second.Pix[i] = 1
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "animation.gif")
	if err := os.WriteFile(filename, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	thumb, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := color.RGBAModel.Convert(thumb.At(0, 0)); got != palette[0] {
		t.Errorf("thumbnail pixel = %v, want first frame %v", got, palette[0])
	}
}

func TestCreateRejectsOversizedHeaderBeforeDecode(t *testing.T) {
	for _, size := range []image.Point{image.Pt(maxInputSide+1, 1), image.Pt(4001, 4000)} {
		// A valid GIF configuration with no image data: full decoding would fail.
		header := make([]byte, 13)
		copy(header, "GIF89a")
		binary.LittleEndian.PutUint16(header[6:8], uint16(size.X))
		binary.LittleEndian.PutUint16(header[8:10], uint16(size.Y))
		filename := filepath.Join(t.TempDir(), "oversized.gif")
		if err := os.WriteFile(filename, header, 0600); err != nil {
			t.Fatal(err)
		}
		if output, err := Create(filename); !errors.Is(err, ErrTooLarge) || output != "" {
			t.Fatalf("Create(%v) = %q, %v; want empty path and ErrTooLarge", size, output, err)
		}
		if _, err := os.Stat(Path(filename)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected thumbnail file: %v", err)
		}
	}
}

func TestCreateRejectsLargeFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "large.png")
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	truncateErr := file.Truncate(maxInputBytes + 1)
	closeErr := file.Close()
	if truncateErr != nil || closeErr != nil {
		t.Fatalf("create sparse file: %v, %v", truncateErr, closeErr)
	}
	if _, err := Create(filename); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Create() error = %v, want ErrTooLarge", err)
	}
}

func TestCreateUnsupportedAndCorrupt(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"document.pdf", []byte("%PDF-1.7\n")},
		{"corrupt.png", encoded.Bytes()[:len(encoded.Bytes())-12]},
	} {
		t.Run(test.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), test.name)
			if err := os.WriteFile(filename, test.data, 0600); err != nil {
				t.Fatal(err)
			}
			if output, err := Create(filename); err == nil || output != "" {
				t.Fatalf("Create() = %q, %v; want failure", output, err)
			}
			entries, err := os.ReadDir(filepath.Dir(filename))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Errorf("leftover thumbnail files: %v", entries)
			}
		})
	}
}

func TestCreateCleansUpTemporaryFileOnPublishFailure(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "upload.png")
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	// A directory at the destination prevents publishing the PNG via rename.
	if err := os.Mkdir(Path(filename), 0700); err != nil {
		t.Fatal(err)
	}
	if output, err := Create(filename); err == nil || output != "" {
		t.Fatalf("Create() = %q, %v; want failure", output, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("temporary thumbnail was not removed: %v", entries)
	}
}
