package proxy

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strconv"

	"golang.org/x/image/draw"
)

// ImageParams holds parsed image transformation parameters from query string.
type ImageParams struct {
	Width   int    // target width (0 = unchanged)
	Height  int    // target height (0 = unchanged)
	Format  string // output format: "jpeg", "png", "webp"
	Quality int    // JPEG quality 1-100 (default 85)
}

// ParseImageParams extracts image optimization parameters from the request URL.
// Returns nil if no image parameters are present.
func ParseImageParams(r *http.Request) *ImageParams {
	q := r.URL.Query()

	wStr := q.Get("w")
	hStr := q.Get("h")
	fmtStr := q.Get("fmt")
	qStr := q.Get("q")

	if wStr == "" && hStr == "" && fmtStr == "" && qStr == "" {
		return nil
	}

	params := &ImageParams{Quality: 85}

	if wStr != "" {
		if v, err := strconv.Atoi(wStr); err == nil && v > 0 {
			params.Width = v
		}
	}
	if hStr != "" {
		if v, err := strconv.Atoi(hStr); err == nil && v > 0 {
			params.Height = v
		}
	}
	if fmtStr != "" {
		switch fmtStr {
		case "jpeg", "jpg":
			params.Format = "jpeg"
		case "png":
			params.Format = "png"
		case "webp":
			params.Format = "webp"
		}
	}
	if qStr != "" {
		if v, err := strconv.Atoi(qStr); err == nil && v >= 1 && v <= 100 {
			params.Quality = v
		}
	}

	return params
}

// ImageCacheKey generates a cache key suffix that includes image parameters.
func ImageCacheKey(baseKey string, params *ImageParams) string {
	if params == nil {
		return baseKey
	}
	suffix := fmt.Sprintf("|img:w=%d,h=%d,fmt=%s,q=%d", params.Width, params.Height, params.Format, params.Quality)
	return baseKey + suffix
}

// TransformImage decodes, resizes, and re-encodes an image according to the given params.
// Returns the transformed image bytes and the appropriate Content-Type.
// If the image cannot be decoded or the format is unsupported (e.g., webp output),
// the original body is returned unchanged.
func TransformImage(body []byte, contentType string, params *ImageParams) ([]byte, string) {
	if params == nil {
		return body, contentType
	}

	// Decode the source image
	src, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		// Cannot decode — return original
		return body, contentType
	}

	// Resize if dimensions specified
	dst := resizeImage(src, params.Width, params.Height)

	// Determine output format
	outFormat := params.Format
	if outFormat == "" {
		// Infer from original content type
		switch {
		case contains(contentType, "png"):
			outFormat = "png"
		default:
			outFormat = "jpeg"
		}
	}

	// WebP encoding not available in standard library; fall back to jpeg
	if outFormat == "webp" {
		outFormat = "jpeg"
	}

	// Encode
	var buf bytes.Buffer
	var outContentType string

	switch outFormat {
	case "png":
		if err := png.Encode(&buf, dst); err != nil {
			return body, contentType
		}
		outContentType = "image/png"
	default: // jpeg
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: params.Quality}); err != nil {
			return body, contentType
		}
		outContentType = "image/jpeg"
	}

	return buf.Bytes(), outContentType
}

// resizeImage scales an image to the target dimensions.
// If both width and height are 0, the original image is returned.
// If only one dimension is specified, the other is computed to preserve aspect ratio.
func resizeImage(src image.Image, width, height int) image.Image {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if width == 0 && height == 0 {
		return src
	}

	// Compute missing dimension to preserve aspect ratio
	if width == 0 {
		width = srcW * height / srcH
	}
	if height == 0 {
		height = srcH * width / srcW
	}

	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.BiLinear.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	return dst
}

// contains is a simple case-insensitive substring check.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchLower(s, substr)
}

func searchLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			sc := s[i+j]
			tc := substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 32
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// IsImageContentType checks if the content type is an image type that we can process.
func IsImageContentType(contentType string) bool {
	return contains(contentType, "image/jpeg") ||
		contains(contentType, "image/png") ||
		contains(contentType, "image/gif")
}
