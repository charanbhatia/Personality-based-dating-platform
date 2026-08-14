package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func sampleImage(t *testing.T, w, h int, encode string) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	var err error
	if encode == ContentTypePNG {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatalf("encode sample: %v", err)
	}
	return buf.Bytes()
}

func decodeBounds(t *testing.T, data []byte) image.Rectangle {
	t.Helper()

	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	return img.Bounds()
}

func TestThumbnailScalesTheLongEdgeAndKeepsAspectRatio(t *testing.T) {
	cases := []struct {
		name          string
		w, h          int
		contentType   string
		wantW, wantH  int
		maxEdgeTarget int
	}{
		{"landscape jpeg", 1200, 600, ContentTypeJPEG, 512, 256, 512},
		{"portrait png", 600, 1200, ContentTypePNG, 256, 512, 512},
		{"square jpeg", 1000, 1000, ContentTypeJPEG, 512, 512, 512},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Thumbnail(sampleImage(t, tc.w, tc.h, tc.contentType), tc.contentType, tc.maxEdgeTarget)
			if err != nil {
				t.Fatalf("thumbnail: %v", err)
			}

			b := decodeBounds(t, out)
			if b.Dx() != tc.wantW || b.Dy() != tc.wantH {
				t.Errorf("size = %dx%d, want %dx%d", b.Dx(), b.Dy(), tc.wantW, tc.wantH)
			}
			if len(out) >= len(sampleImage(t, tc.w, tc.h, tc.contentType)) && tc.contentType == ContentTypePNG {
				t.Log("thumbnail is not smaller than the source, which is acceptable for synthetic noise")
			}
		})
	}
}

func TestThumbnailDoesNotUpscaleSmallImages(t *testing.T) {
	src := sampleImage(t, 100, 80, ContentTypeJPEG)

	out, err := Thumbnail(src, ContentTypeJPEG, 512)
	if err != nil {
		t.Fatalf("thumbnail: %v", err)
	}

	b := decodeBounds(t, out)
	if b.Dx() != 100 || b.Dy() != 80 {
		t.Errorf("size = %dx%d, want the original 100x80", b.Dx(), b.Dy())
	}
}

func TestThumbnailRejectsTypesItCannotDecode(t *testing.T) {
	if Resizable(ContentTypeWebP) {
		t.Error("webp must not be reported as resizable while decoding is unsupported")
	}
	if _, err := Thumbnail([]byte("not an image"), ContentTypeWebP, 512); err == nil {
		t.Error("expected an error for an undecodable content type")
	}
	if _, err := Thumbnail([]byte("not an image"), ContentTypeJPEG, 512); err == nil {
		t.Error("expected an error for corrupt jpeg data")
	}
}
