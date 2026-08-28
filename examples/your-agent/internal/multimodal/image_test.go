package multimodal

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

var testPNG = []byte("\x89PNG\r\n\x1a\nmultimodal-test")

func TestNormalizeRawBase64Image(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString(testPNG)
	image, err := Normalize(raw, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if image.MediaType != "image/png" || image.Size != int64(len(testPNG)) {
		t.Fatalf("image = %+v", image)
	}
	if !strings.HasPrefix(image.DataURL, "data:image/png;base64,") {
		t.Fatalf("data URL = %q", image.DataURL)
	}
}

func TestNormalizeRejectsSpoofedMediaType(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString(testPNG)
	_, err := Normalize("data:image/jpeg;base64,"+raw, 1024)
	if err == nil || !strings.Contains(err.Error(), "media type mismatch") {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeRejectsUnsupportedImage(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString([]byte("plain text"))
	_, err := Normalize(raw, 1024)
	if !errors.Is(err, ErrUnsupportedImageType) {
		t.Fatalf("error = %v", err)
	}
}

func TestNormalizeAllEnforcesCountAndTotalLimits(t *testing.T) {
	raw := base64.StdEncoding.EncodeToString(testPNG)
	if _, err := NormalizeAll([]string{raw, raw}, 1, 1024, 2048); err == nil {
		t.Fatal("expected image count error")
	}
	if _, err := NormalizeAll([]string{raw, raw}, 2, 1024, int64(len(testPNG))); err == nil {
		t.Fatal("expected total image size error")
	}
}
