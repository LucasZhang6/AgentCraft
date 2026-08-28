package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/session"
)

func TestSessionWebAPIManagesStructuredSessions(t *testing.T) {
	root := t.TempDir()
	server, err := New(Config{DataDir: filepath.Join(root, "data"), WorkDir: root})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	handler := server.Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/sessions", strings.NewReader(`{"title":"Research"}`)))
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var created session.Status
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.SessionID == "" || created.Title != "Research" {
		t.Fatalf("unexpected session: %+v", created)
	}

	patchBody := []byte(`{"session_id":"` + created.SessionID + `","title":"Renamed"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/api/sessions", bytes.NewReader(patchBody)))
	if response.Code != http.StatusOK {
		t.Fatalf("rename status=%d body=%s", response.Code, response.Body.String())
	}

	forkBody := []byte(`{"session_id":"` + created.SessionID + `"}`)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/sessions/fork", bytes.NewReader(forkBody)))
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), created.SessionID) {
		t.Fatalf("fork status=%d body=%s", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/sessions/messages?session_id="+created.SessionID, nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"messages"`) {
		t.Fatalf("messages status=%d body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/sessions/events?session_id="+created.SessionID, nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"events"`) {
		t.Fatalf("events status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestServerRestoresInterruptedTask(t *testing.T) {
	root := t.TempDir()
	config := Config{DataDir: filepath.Join(root, "data"), WorkDir: root}
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := server.taskStore.save(context.Background(), taskRecord{
		ID: "task-restored", SessionID: "session-restored", Status: "running", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}

	server, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	status := server.task("task-restored").snapshot()
	if status.Status != "interrupted" || !status.Complete || status.Success {
		t.Fatalf("unexpected restored task: %+v", status)
	}
}
