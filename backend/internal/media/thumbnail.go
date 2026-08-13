package media

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
)

const (
	ContentTypeJPEG = "image/jpeg"
	ContentTypePNG  = "image/png"
	ContentTypeWebP = "image/webp"

	thumbnailQuality = 82
)

// Resizable reports whether a thumbnail can be produced for a content type.
// WebP decoding is not in the standard library, so those uploads reuse the
// original as their thumbnail.
func Resizable(contentType string) bool {
	return contentType == ContentTypeJPEG || contentType == ContentTypePNG
}

// Thumbnail scales an image so its longest edge is at most maxEdge, preserving
// aspect ratio, and encodes the result as JPEG. Images already within the
// bound are re-encoded rather than upscaled.
func Thumbnail(original []byte, contentType string, maxEdge int) ([]byte, error) {
	var (
		src image.Image
		err error
	)
	switch contentType {
	case ContentTypeJPEG:
		src, err = jpeg.Decode(bytes.NewReader(original))
	case ContentTypePNG:
		src, err = png.Decode(bytes.NewReader(original))
	default:
		return nil, fmt.Errorf("cannot resize content type %q", contentType)
	}
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	dst := scale(src, maxEdge)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: thumbnailQuality}); err != nil {
		return nil, fmt.Errorf("encode thumbnail: %w", err)
	}
	return buf.Bytes(), nil
}

func scale(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}
	if w <= maxEdge && h <= maxEdge {
		return src
	}

	dw, dh := w, h
	if w >= h {
		dw = maxEdge
		dh = int(float64(h) * float64(maxEdge) / float64(w))
	} else {
		dh = maxEdge
		dw = int(float64(w) * float64(maxEdge) / float64(h))
	}
	dw, dh = max(dw, 1), max(dh, 1)

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xRatio := float64(w) / float64(dw)
	yRatio := float64(h) / float64(dh)

	// Box sampling: average the source pixels covered by each destination
	// pixel, which avoids the aliasing a nearest-neighbour pick would produce.
	for y := 0; y < dh; y++ {
		y0 := b.Min.Y + int(float64(y)*yRatio)
		y1 := b.Min.Y + int(float64(y+1)*yRatio)
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dw; x++ {
			x0 := b.Min.X + int(float64(x)*xRatio)
			x1 := b.Min.X + int(float64(x+1)*xRatio)
			if x1 <= x0 {
				x1 = x0 + 1
			}

			var rs, gs, bs, as, n uint64
			for sy := y0; sy < y1 && sy < b.Max.Y; sy++ {
				for sx := x0; sx < x1 && sx < b.Max.X; sx++ {
					r, g, bl, a := src.At(sx, sy).RGBA()
					rs += uint64(r)
					gs += uint64(g)
					bs += uint64(bl)
					as += uint64(a)
					n++
				}
			}
			if n == 0 {
				continue
			}

			i := dst.PixOffset(x, y)
			dst.Pix[i] = uint8(rs / n >> 8)
			dst.Pix[i+1] = uint8(gs / n >> 8)
			dst.Pix[i+2] = uint8(bs / n >> 8)
			dst.Pix[i+3] = uint8(as / n >> 8)
		}
	}
	return dst
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
