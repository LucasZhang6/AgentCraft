package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/server"
)

func TestServerExecutesTaskAndContinuesSession(t *testing.T) {
	service, err := server.New(server.Config{DataDir: t.TempDir(), Provider: "demo"})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer service.Close()
	handler := service.Handler()

	first := execute(t, handler, server.ExecuteRequest{Message: "解读 Agent Memory", Async: false})
	if !first.Success || first.SessionID == "" || first.Result == "" {
		t.Fatalf("first response = %#v", first)
	}
	second := execute(t, handler, server.ExecuteRequest{
		Message: "继续说明工程限制", SessionID: first.SessionID, Async: false,
	})
	if !second.Success || second.SessionID != first.SessionID {
		t.Fatalf("second response = %#v", second)
	}

	request := httptest.NewRequest(http.MethodGet,
		"/api/session/status?session_id="+url.QueryEscape(first.SessionID), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("session status code = %d body=%s", response.Code, response.Body.String())
	}
	var status struct {
		MessageCount        int    `json:"messageCount"`
		TurnCount           int    `json:"turnCount"`
		LastTurnStatus      string `json:"lastTurnStatus"`
		CanonicalEventCount int    `json:"canonicalEventCount"`
		TotalToolCalls      int    `json:"totalToolCalls"`
	}
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.MessageCount < 12 || status.TurnCount != 2 || status.LastTurnStatus != "completed" ||
		status.CanonicalEventCount == 0 || status.TotalToolCalls != 4 {
		t.Fatalf("structured session status = %#v", status)
	}
}

func TestServerWebGoalPlanAndSSE(t *testing.T) {
	service, err := server.New(server.Config{DataDir: t.TempDir(), Provider: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	handler := service.Handler()

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Your Agent") {
		t.Fatalf("web page code=%d body=%q", page.Code, page.Body.String())
	}

	created := execute(t, handler, server.ExecuteRequest{Message: "解读 Agent Memory", Mode: "goal", Async: true})
	stream := httptest.NewRecorder()
	handler.ServeHTTP(stream, httptest.NewRequest(http.MethodGet, "/api/agent/events?task_id="+url.QueryEscape(created.TaskID), nil))
	if stream.Code != http.StatusOK || !strings.Contains(stream.Body.String(), `"type":"status"`) || !strings.Contains(stream.Body.String(), `"complete":true`) {
		t.Fatalf("SSE code=%d body=%s", stream.Code, stream.Body.String())
	}
	resumed := httptest.NewRecorder()
	resumeRequest := httptest.NewRequest(http.MethodGet, "/api/agent/events?task_id="+url.QueryEscape(created.TaskID)+"&after=1", nil)
	handler.ServeHTTP(resumed, resumeRequest)
	if resumed.Code != http.StatusOK || strings.Contains(resumed.Body.String(), "id: 1\n") || !strings.Contains(resumed.Body.String(), "id: 2\n") {
		t.Fatalf("resumed SSE code=%d body=%s", resumed.Code, resumed.Body.String())
	}

	goalStatus := httptest.NewRecorder()
	handler.ServeHTTP(goalStatus, httptest.NewRequest(http.MethodGet, "/api/goal/status?session_id="+url.QueryEscape(created.SessionID), nil))
	if goalStatus.Code != http.StatusOK || !strings.Contains(goalStatus.Body.String(), `"state":"achieved"`) {
		t.Fatalf("goal status code=%d body=%s", goalStatus.Code, goalStatus.Body.String())
	}

	plan := httptest.NewRecorder()
	handler.ServeHTTP(plan, httptest.NewRequest(http.MethodGet, "/api/plan/latest?session_id="+url.QueryEscape(created.SessionID), nil))
	if plan.Code != http.StatusOK || !strings.Contains(plan.Body.String(), `"status":"completed"`) {
		t.Fatalf("plan code=%d body=%s", plan.Code, plan.Body.String())
	}
}

func TestServerAccessIDLogin(t *testing.T) {
	service, err := server.New(server.Config{DataDir: t.TempDir(), Provider: "demo", AccessID: "secret-access"})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer service.Close()
	handler := service.Handler()

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/agent/status?task_id=x", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized code = %d", unauthorized.Code)
	}

	loginBody, _ := json.Marshal(map[string]string{"access_id": "secret-access"})
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody)))
	if login.Code != http.StatusOK {
		t.Fatalf("login code = %d body=%s", login.Code, login.Body.String())
	}
	var auth struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(login.Body).Decode(&auth)
	request := httptest.NewRequest(http.MethodGet, "/api/agent/status?task_id=x", nil)
	request.Header.Set("Authorization", "Bearer "+auth.Token)
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusNotFound {
		t.Fatalf("authorized code = %d body=%s", authorized.Code, authorized.Body.String())
	}
}

func TestServerCancelStopsProviderRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() {
		close(release)
		provider.Close()
	}()

	service, err := server.New(server.Config{
		DataDir: t.TempDir(), Provider: "openai", Model: "test-model",
		APIKey: "test-key", BaseURL: provider.URL,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	defer service.Close()
	handler := service.Handler()
	created := execute(t, handler, server.ExecuteRequest{Message: "cancel me", Async: true})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider request did not start")
	}
	cancel := httptest.NewRecorder()
	handler.ServeHTTP(cancel, httptest.NewRequest(http.MethodPost,
		"/api/agent/cancel?task_id="+url.QueryEscape(created.TaskID), nil))
	if cancel.Code != http.StatusOK {
		t.Fatalf("cancel code = %d body=%s", cancel.Code, cancel.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		statusRequest := httptest.NewRequest(http.MethodGet,
			"/api/agent/status?task_id="+url.QueryEscape(created.TaskID), nil)
		statusRecorder := httptest.NewRecorder()
		handler.ServeHTTP(statusRecorder, statusRequest)
		var status server.StatusResponse
		if err := json.NewDecoder(statusRecorder.Body).Decode(&status); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if status.Complete {
			if status.Status != "canceled" || status.Success {
				t.Fatalf("status = %#v", status)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not reach canceled state")
}

func execute(t *testing.T, handler http.Handler, input server.ExecuteRequest) server.ExecuteResponse {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal execute: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/agent/execute", bytes.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK && response.Code != http.StatusAccepted {
		t.Fatalf("execute code = %d body=%s", response.Code, response.Body.String())
	}
	var output server.ExecuteResponse
	if err := json.NewDecoder(response.Body).Decode(&output); err != nil {
		t.Fatalf("decode execute: %v", err)
	}
	return output
}
