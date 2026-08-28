package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileAPIReadsWritesAndRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	server, err := New(Config{DataDir: filepath.Join(root, "data"), WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	handler := server.Handler()

	request := httptest.NewRequest(http.MethodPut, "/api/files/content", strings.NewReader(`{"path":"notes/test.md","content":"hello"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/files/content?path=notes/test.md", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hello") {
		t.Fatalf("read status=%d body=%s", response.Code, response.Body.String())
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/files?path=outside", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected symlink escape rejection, status=%d body=%s", response.Code, response.Body.String())
	}
}
