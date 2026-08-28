package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleSessions(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		items, err := s.sessions.List(request.Context(), queryLimit(request, 100))
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		query := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("q")))
		if query != "" {
			filtered := items[:0]
			for _, item := range items {
				if strings.Contains(strings.ToLower(item.SessionID), query) || strings.Contains(strings.ToLower(item.Title), query) {
					filtered = append(filtered, item)
				}
			}
			items = filtered
		}
		writeJSON(writer, http.StatusOK, map[string]any{"sessions": items})
	case http.MethodPost:
		var payload struct {
			SessionID string `json:"session_id"`
			Title     string `json:"title"`
		}
		if err := decodeOptionalJSON(request, &payload); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		id, err := s.sessions.Ensure(request.Context(), payload.SessionID)
		if err == nil && strings.TrimSpace(payload.Title) != "" {
			err = s.sessions.UpdateTitle(request.Context(), id, payload.Title)
		}
		if err != nil {
			writeJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		status, err := s.sessions.Status(request.Context(), id)
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(writer, http.StatusCreated, status)
	case http.MethodPatch:
		var payload struct {
			SessionID string `json:"session_id"`
			Title     string `json:"title"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if err := s.sessions.UpdateTitle(request.Context(), payload.SessionID, payload.Title); err != nil {
			writeStoreError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"success": true})
	case http.MethodDelete:
		sessionID := strings.TrimSpace(request.URL.Query().Get("session_id"))
		s.mu.RLock()
		active := false
		for _, current := range s.tasks {
			status := current.snapshot()
			if status.SessionID == sessionID && !status.Complete {
				active = true
				break
			}
		}
		s.mu.RUnlock()
		if active {
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "session has an active task"})
			return
		}
		if err := s.sessions.Clear(request.Context(), sessionID); err != nil {
			writeStoreError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"success": true})
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleSessionMessages(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	items, err := s.sessions.Messages(request.Context(), request.URL.Query().Get("session_id"))
	if err != nil {
		writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"messages": items})
}

func (s *Server) handleSessionEvents(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	items, err := s.sessions.CanonicalEvents(request.Context(), request.URL.Query().Get("session_id"))
	if err != nil {
		writeStoreError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"events": items})
}

func (s *Server) handleSessionFork(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		SessionID string `json:"session_id"`
		Title     string `json:"title"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	id, err := s.sessions.Fork(request.Context(), payload.SessionID, payload.Title)
	if err != nil {
		writeStoreError(writer, err)
		return
	}
	status, err := s.sessions.Status(request.Context(), id)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusCreated, status)
}

func (s *Server) handleTasks(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		s.mu.RLock()
		items := make([]StatusResponse, 0, len(s.tasks))
		for _, current := range s.tasks {
			items = append(items, current.snapshot())
		}
		s.mu.RUnlock()
		sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
		limit := queryLimit(request, 100)
		if len(items) > limit {
			items = items[:limit]
		}
		writeJSON(writer, http.StatusOK, map[string]any{"tasks": items})
	case http.MethodDelete:
		id := strings.TrimSpace(request.URL.Query().Get("task_id"))
		current := s.task(id)
		if current == nil {
			writeJSON(writer, http.StatusNotFound, map[string]string{"error": "task not found"})
			return
		}
		if !current.snapshot().Complete {
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "active task cannot be deleted"})
			return
		}
		if err := s.taskStore.delete(request.Context(), id); err != nil {
			writeStoreError(writer, err)
			return
		}
		s.mu.Lock()
		delete(s.tasks, id)
		s.mu.Unlock()
		writeJSON(writer, http.StatusOK, map[string]any{"success": true})
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleSkills(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	name := strings.TrimSpace(request.URL.Query().Get("name"))
	if name == "" {
		writeJSON(writer, http.StatusOK, map[string]any{"skills": s.skills.List(), "warnings": s.skills.Warnings()})
		return
	}
	skill, ok := s.skills.Get(name)
	if !ok {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "skill not found"})
		return
	}
	writeJSON(writer, http.StatusOK, skill)
}

func (s *Server) handleSubagents(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		id := strings.TrimSpace(request.URL.Query().Get("id"))
		if id != "" {
			record, err := s.subagents.Get(request.Context(), id)
			if err != nil {
				writeStoreError(writer, err)
				return
			}
			writeJSON(writer, http.StatusOK, record)
			return
		}
		items, err := s.subagents.List(request.Context(), request.URL.Query().Get("session_id"), request.URL.Query().Get("run_id"), queryLimit(request, 100))
		if err != nil {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"subagents": items})
	case http.MethodPost:
		var payload struct {
			ID          string `json:"id"`
			Action      string `json:"action"`
			WaitSeconds int    `json:"wait_seconds"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		var record any
		var err error
		switch strings.ToLower(strings.TrimSpace(payload.Action)) {
		case "cancel":
			record, err = s.subagents.Cancel(request.Context(), payload.ID)
		case "wait":
			if payload.WaitSeconds <= 0 || payload.WaitSeconds > 60 {
				payload.WaitSeconds = 30
			}
			ctx, cancel := context.WithTimeout(request.Context(), time.Duration(payload.WaitSeconds)*time.Second)
			defer cancel()
			record, err = s.subagents.Wait(ctx, payload.ID)
		default:
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "action must be cancel or wait"})
			return
		}
		if err != nil {
			writeStoreError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, record)
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func queryLimit(request *http.Request, fallback int) int {
	value, err := strconv.Atoi(request.URL.Query().Get("limit"))
	if err != nil || value <= 0 || value > 1000 {
		return fallback
	}
	return value
}

func decodeOptionalJSON(request *http.Request, target any) error {
	if request.Body == nil || request.ContentLength == 0 {
		return nil
	}
	return json.NewDecoder(request.Body).Decode(target)
}

func writeStoreError(writer http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
	}
	writeJSON(writer, status, map[string]string{"error": err.Error()})
}
