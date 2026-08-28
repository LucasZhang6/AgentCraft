package multimodal

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	DefaultMaxImages           = 4
	DefaultMaxImageBytes int64 = 8 << 20
	DefaultMaxTotalBytes int64 = 20 << 20
)

var ErrUnsupportedImageType = errors.New("unsupported image type")

// Image is a validated image represented in the formats used by the supported
// LLM APIs. DataURL and Base64 always contain canonical standard-base64 data.
type Image struct {
	DataURL   string
	MediaType string
	Base64    string
	Size      int64
}

func FromBytes(data []byte, declaredType string, maxBytes int64) (Image, error) {
	if len(data) == 0 {
		return Image{}, errors.New("image is empty")
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return Image{}, fmt.Errorf("image exceeds %d-byte limit", maxBytes)
	}

	detectedType := detectImageType(data)
	if detectedType == "" {
		return Image{}, ErrUnsupportedImageType
	}
	declaredType = normalizeMediaType(declaredType)
	if declaredType != "" && declaredType != "application/octet-stream" && declaredType != detectedType {
		return Image{}, fmt.Errorf("image media type mismatch: declared %s, detected %s", declaredType, detectedType)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return Image{
		DataURL:   "data:" + detectedType + ";base64," + encoded,
		MediaType: detectedType,
		Base64:    encoded,
		Size:      int64(len(data)),
	}, nil
}

// Normalize accepts either a data URL or raw standard base64 and returns a
// canonical data URL after decoding and validating the actual image bytes.
func Normalize(input string, maxBytes int64) (Image, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Image{}, errors.New("image is empty")
	}

	declaredType := ""
	encoded := input
	if strings.HasPrefix(strings.ToLower(input), "data:") {
		header, payload, ok := strings.Cut(input, ",")
		if !ok {
			return Image{}, errors.New("invalid image data URL")
		}
		parts := strings.Split(header[5:], ";")
		if len(parts) < 2 || !strings.EqualFold(parts[len(parts)-1], "base64") {
			return Image{}, errors.New("image data URL must use base64 encoding")
		}
		declaredType = parts[0]
		encoded = payload
	}

	if maxBytes > 0 && int64(base64.StdEncoding.DecodedLen(len(encoded))) > maxBytes+2 {
		return Image{}, fmt.Errorf("image exceeds %d-byte limit", maxBytes)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Image{}, fmt.Errorf("decode image base64: %w", err)
	}
	return FromBytes(data, declaredType, maxBytes)
}

func NormalizeAll(inputs []string, maxImages int, maxEachBytes, maxTotalBytes int64) ([]string, error) {
	if maxImages > 0 && len(inputs) > maxImages {
		return nil, fmt.Errorf("too many images: got %d, maximum is %d", len(inputs), maxImages)
	}
	normalized := make([]string, 0, len(inputs))
	var total int64
	for i, input := range inputs {
		image, err := Normalize(input, maxEachBytes)
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", i+1, err)
		}
		total += image.Size
		if maxTotalBytes > 0 && total > maxTotalBytes {
			return nil, fmt.Errorf("images exceed %d-byte total limit", maxTotalBytes)
		}
		normalized = append(normalized, image.DataURL)
	}
	return normalized, nil
}

func detectImageType(data []byte) string {
	if len(data) >= 12 &&
		string(data[:4]) == "RIFF" &&
		string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	switch normalizeMediaType(http.DetectContentType(data)) {
	case "image/jpeg":
		return "image/jpeg"
	case "image/png":
		return "image/png"
	case "image/gif":
		return "image/gif"
	default:
		return ""
	}
}

func normalizeMediaType(value string) string {
	value = strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	if value == "image/jpg" {
		return "image/jpeg"
	}
	return value
}
