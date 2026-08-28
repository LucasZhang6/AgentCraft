package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const maxEditorBytes = 2 * 1024 * 1024

type fileEntry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	Directory  bool      `json:"directory"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

func (s *Server) handleFiles(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	path, err := s.workspacePath(request.URL.Query().Get("path"), false)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	items := make([]fileEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		fullPath := filepath.Join(path, entry.Name())
		items = append(items, fileEntry{
			Name: entry.Name(), Path: s.workspaceRelative(fullPath), Directory: entry.IsDir(), Size: info.Size(), ModifiedAt: info.ModTime().UTC(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Directory != items[j].Directory {
			return items[i].Directory
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	writeJSON(writer, http.StatusOK, map[string]any{"path": s.workspaceRelative(path), "entries": items})
}

func (s *Server) handleFileContent(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		path, err := s.workspacePath(request.URL.Query().Get("path"), false)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		data, err := readEditorFile(path)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"path": s.workspaceRelative(path), "content": string(data), "size": len(data)})
	case http.MethodPut:
		var payload struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if len(payload.Content) > maxEditorBytes {
			writeJSON(writer, http.StatusRequestEntityTooLarge, map[string]string{"error": "file exceeds editor limit"})
			return
		}
		path, err := s.workspacePath(payload.Path, true)
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		temp, err := os.CreateTemp(filepath.Dir(path), ".your-agent-save-*")
		if err == nil {
			_, err = temp.WriteString(payload.Content)
		}
		if closeErr := closeFile(temp); err == nil {
			err = closeErr
		}
		if err == nil {
			err = replaceFile(temp.Name(), path)
		}
		if err != nil {
			if temp != nil {
				_ = os.Remove(temp.Name())
			}
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"success": true, "path": s.workspaceRelative(path), "size": len(payload.Content)})
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleFileDownload(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	path, err := s.workspacePath(request.URL.Query().Get("path"), false)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}
	writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
	http.ServeFile(writer, request, path)
}

func (s *Server) workspacePath(value string, allowMissing bool) (string, error) {
	root, err := filepath.Abs(filepath.Clean(s.config.WorkDir))
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = "."
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path, err = filepath.Abs(filepath.Clean(path))
	if err != nil || !pathWithin(root, path) {
		return "", errors.New("path escapes workspace")
	}
	check := path
	if allowMissing {
		for {
			if _, statErr := os.Lstat(check); statErr == nil {
				break
			}
			parent := filepath.Dir(check)
			if parent == check {
				return "", errors.New("cannot resolve path parent")
			}
			check = parent
		}
	}
	resolved, err := filepath.EvalSymlinks(check)
	if err != nil || !pathWithin(root, resolved) {
		return "", errors.New("path resolves outside workspace")
	}
	return path, nil
}

func (s *Server) workspaceRelative(path string) string {
	root, _ := filepath.Abs(filepath.Clean(s.config.WorkDir))
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." {
		return "."
	}
	return filepath.ToSlash(relative)
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func readEditorFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	if info.Size() > maxEditorBytes {
		return nil, errors.New("file exceeds editor limit; use download instead")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, errors.New("binary file cannot be opened in the editor")
	}
	return data, nil
}

func closeFile(file *os.File) error {
	if file == nil {
		return nil
	}
	return file.Close()
}
