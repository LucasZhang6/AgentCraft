package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/agent"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/app"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	goalstore "github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/goal"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/multimodal"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/planning"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/session"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/skills"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/subagent"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/tools"
)

//go:embed web/index.html
var webIndex []byte

type Config struct {
	DataDir               string
	WorkDir               string
	AccessID              string
	Provider              string
	Model                 string
	FallbackModels        []string
	APIKey                string
	BaseURL               string
	MaxOutputTokens       int
	MaxSteps              int
	MaxGoalTurns          int
	TokenBudget           int
	MaxRecentObservations int
	ToolTimeout           time.Duration
	ToolOutputBytes       int
	MaxQueuedTasks        int
	MaxTasksPerSession    int
	MaxConcurrentTasks    int
	Session               session.Config
	Logger                *log.Logger
}

type Server struct {
	config    Config
	sessions  *session.Store
	goals     *goalstore.Store
	plans     *planning.Store
	taskStore *taskStore
	queue     *taskQueue
	subagents *subagent.Manager
	skills    *skills.Manager
	logger    *log.Logger

	mu          sync.RWMutex
	tasks       map[string]*task
	tokens      map[string]struct{}
	terminalsMu sync.Mutex
	terminals   map[io.Closer]struct{}
	terminalsWG sync.WaitGroup
	stopping    bool
}

type task struct {
	mu               sync.RWMutex
	id               string
	sessionID        string
	userMessage      string
	goalID           string
	status           string
	result           string
	err              string
	messages         []string
	pendingApproval  *PendingApproval
	approveRemaining bool
	approval         chan approvalDecision
	cancel           context.CancelFunc
	done             chan struct{}
	doneOnce         sync.Once
	complete         bool
	success          bool
	createdAt        time.Time
	updatedAt        time.Time
	store            *taskStore
	logger           *log.Logger
}

type approvalDecision struct {
	toolID   string
	approved bool
}

type ExecuteRequest struct {
	Message      string   `json:"message"`
	Images       []string `json:"images,omitempty"`
	Mode         string   `json:"mode,omitempty"`
	AutoApprove  bool     `json:"auto_approve"`
	Async        bool     `json:"async"`
	SessionID    string   `json:"session_id,omitempty"`
	ResumePlanID string   `json:"resume_plan_id,omitempty"`
}

type ExecuteResponse struct {
	Success   bool     `json:"success"`
	TaskID    string   `json:"task_id"`
	SessionID string   `json:"session_id"`
	Result    string   `json:"result"`
	Messages  []string `json:"messages"`
	Error     string   `json:"error"`
}

type PendingApproval struct {
	ToolID        string `json:"tool_id"`
	ToolName      string `json:"tool_name"`
	AgentName     string `json:"agent_name"`
	ParamsPreview string `json:"params_preview"`
}

type StatusResponse struct {
	TaskID          string           `json:"task_id"`
	SessionID       string           `json:"session_id"`
	UserMessage     string           `json:"user_message,omitempty"`
	GoalID          string           `json:"goal_id,omitempty"`
	Status          string           `json:"status"`
	Messages        []string         `json:"messages"`
	Complete        bool             `json:"complete"`
	Success         bool             `json:"success"`
	Error           string           `json:"error"`
	Result          string           `json:"result"`
	PendingApproval *PendingApproval `json:"pending_approval,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

func New(config Config) (*Server, error) {
	if strings.TrimSpace(config.DataDir) == "" {
		config.DataDir = ".agent-data"
	}
	if strings.TrimSpace(config.WorkDir) == "" {
		config.WorkDir = "."
	}
	if config.Logger == nil {
		config.Logger = log.New(os.Stderr, "[your-agent-server] ", log.LstdFlags)
	}
	if config.Session.Summarizer == nil {
		summarizer, err := app.NewSessionSummarizer(app.Config{
			Provider: config.Provider, Model: config.Model, APIKey: config.APIKey,
			BaseURL: config.BaseURL, FallbackModels: config.FallbackModels, HTTPClient: nil,
		})
		if err != nil {
			return nil, fmt.Errorf("initialize session summarizer: %w", err)
		}
		config.Session.Summarizer = summarizer
	}
	sessions, err := session.NewStore(filepath.Join(config.DataDir, "sessions.db"), config.Session)
	if err != nil {
		return nil, err
	}
	goals, err := goalstore.NewStore(filepath.Join(config.DataDir, "goals.db"))
	if err != nil {
		_ = sessions.Close()
		return nil, err
	}
	plans, err := planning.NewStore(filepath.Join(config.DataDir, "plans.db"))
	if err != nil {
		_ = sessions.Close()
		_ = goals.Close()
		return nil, err
	}
	tasks, err := newTaskStore(filepath.Join(config.DataDir, "tasks.db"))
	if err != nil {
		_ = sessions.Close()
		_ = goals.Close()
		_ = plans.Close()
		return nil, err
	}
	if err := tasks.interruptActive(context.Background()); err != nil {
		_ = sessions.Close()
		_ = goals.Close()
		_ = plans.Close()
		_ = tasks.close()
		return nil, err
	}
	subagents, err := subagent.NewManager(filepath.Join(config.DataDir, "subagents.db"))
	if err != nil {
		_ = sessions.Close()
		_ = goals.Close()
		_ = plans.Close()
		_ = tasks.close()
		return nil, err
	}
	skillManager := skills.NewManager(config.WorkDir)
	if err := skillManager.LoadAll(); err != nil {
		_ = sessions.Close()
		_ = goals.Close()
		_ = plans.Close()
		_ = tasks.close()
		_ = subagents.Close()
		return nil, err
	}
	server := &Server{
		config: config, sessions: sessions, goals: goals, plans: plans, logger: config.Logger,
		taskStore: tasks, subagents: subagents, skills: skillManager,
		tasks: make(map[string]*task), tokens: make(map[string]struct{}),
		terminals: make(map[io.Closer]struct{}),
	}
	restored, err := tasks.list(context.Background(), 500)
	if err != nil {
		_ = server.Close()
		return nil, err
	}
	for _, record := range restored {
		current := taskFromRecord(record, tasks, config.Logger)
		server.tasks[current.id] = current
	}
	server.queue = newTaskQueue(config.MaxConcurrentTasks, config.MaxQueuedTasks, config.MaxTasksPerSession, func(item *queuedTask) {
		server.runTask(item.ctx, item.task, item.input, item.images)
	})
	return server, nil
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	s.closeTerminalConnections()
	s.terminalsWG.Wait()
	if s.queue != nil {
		s.queue.Close()
	}
	var sessionErr, goalErr, planErr, taskErr, subagentErr error
	if s.sessions != nil {
		sessionErr = s.sessions.Close()
	}
	if s.goals != nil {
		goalErr = s.goals.Close()
	}
	if s.plans != nil {
		planErr = s.plans.Close()
	}
	if s.taskStore != nil {
		taskErr = s.taskStore.close()
	}
	if s.subagents != nil {
		subagentErr = s.subagents.Close()
	}
	return errors.Join(sessionErr, goalErr, planErr, taskErr, subagentErr)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWeb)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/api/auth/login", s.handleLogin)
	mux.Handle("/api/agent/execute", s.authorize(http.HandlerFunc(s.handleExecute)))
	mux.Handle("/api/agent/status", s.authorize(http.HandlerFunc(s.handleStatus)))
	mux.Handle("/api/agent/approve", s.authorize(http.HandlerFunc(s.handleApprove)))
	mux.Handle("/api/agent/cancel", s.authorize(http.HandlerFunc(s.handleCancel)))
	mux.Handle("/api/agent/events", s.authorize(http.HandlerFunc(s.handleEvents)))
	mux.Handle("/api/session/status", s.authorize(http.HandlerFunc(s.handleSessionStatus)))
	mux.Handle("/api/sessions", s.authorize(http.HandlerFunc(s.handleSessions)))
	mux.Handle("/api/sessions/messages", s.authorize(http.HandlerFunc(s.handleSessionMessages)))
	mux.Handle("/api/sessions/events", s.authorize(http.HandlerFunc(s.handleSessionEvents)))
	mux.Handle("/api/sessions/fork", s.authorize(http.HandlerFunc(s.handleSessionFork)))
	mux.Handle("/api/tasks", s.authorize(http.HandlerFunc(s.handleTasks)))
	mux.Handle("/api/skills", s.authorize(http.HandlerFunc(s.handleSkills)))
	mux.Handle("/api/subagents", s.authorize(http.HandlerFunc(s.handleSubagents)))
	mux.Handle("/api/files", s.authorize(http.HandlerFunc(s.handleFiles)))
	mux.Handle("/api/files/content", s.authorize(http.HandlerFunc(s.handleFileContent)))
	mux.Handle("/api/files/download", s.authorize(http.HandlerFunc(s.handleFileDownload)))
	mux.Handle("/api/terminal/ws", s.authorize(http.HandlerFunc(s.handleTerminalWS)))
	mux.Handle("/api/goal/status", s.authorize(http.HandlerFunc(s.handleGoalStatus)))
	mux.Handle("/api/goal/action", s.authorize(http.HandlerFunc(s.handleGoalAction)))
	mux.Handle("/api/plan/latest", s.authorize(http.HandlerFunc(s.handlePlanLatest)))
	mux.Handle("/api/plan/accept", s.authorize(http.HandlerFunc(s.handlePlanAccept)))
	return limitBody(mux, 25<<20)
}

func (s *Server) handleWeb(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	_, _ = writer.Write(webIndex)
}

func (s *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogin(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		AccessID string `json:"access_id"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if s.config.AccessID != "" && subtle.ConstantTimeCompare([]byte(payload.AccessID), []byte(s.config.AccessID)) != 1 {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "invalid access id"})
		return
	}
	token, err := randomID("token_", 24)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	s.tokens[token] = struct{}{}
	s.mu.Unlock()
	writeJSON(writer, http.StatusOK, map[string]string{"token": token})
}

func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if s.config.AccessID == "" {
			next.ServeHTTP(writer, request)
			return
		}
		token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			token = request.URL.Query().Get("token")
		}
		s.mu.RLock()
		_, allowed := s.tokens[token]
		s.mu.RUnlock()
		if !allowed {
			writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (s *Server) handleExecute(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var input ExecuteRequest
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, ExecuteResponse{Error: "invalid JSON"})
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	input.ResumePlanID = strings.TrimSpace(input.ResumePlanID)
	if input.Message == "" && len(input.Images) == 0 && input.ResumePlanID == "" {
		writeJSON(writer, http.StatusBadRequest, ExecuteResponse{Error: "message, images, or resume_plan_id are required"})
		return
	}
	images, err := multimodal.NormalizeAll(
		input.Images, multimodal.DefaultMaxImages, multimodal.DefaultMaxImageBytes, multimodal.DefaultMaxTotalBytes,
	)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, ExecuteResponse{Error: err.Error()})
		return
	}
	sessionID, err := s.sessions.Ensure(request.Context(), input.SessionID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, ExecuteResponse{Error: err.Error()})
		return
	}
	userMessage := input.Message
	if input.ResumePlanID != "" && userMessage == "" {
		userMessage = "Resume persisted plan " + input.ResumePlanID
		input.Message = userMessage
	}
	goalID := ""
	if input.ResumePlanID != "" {
		// A persisted plan owns its original objective and goal state.
	} else if strings.EqualFold(strings.TrimSpace(input.Mode), "goal") {
		item, goalErr := s.goals.Set(request.Context(), sessionID, input.Message)
		if goalErr != nil {
			writeJSON(writer, http.StatusConflict, ExecuteResponse{SessionID: sessionID, Error: goalErr.Error()})
			return
		}
		goalID = item.ID
		input.Message = item.ContinuationPrompt("Start the goal and verify every completion condition.")
	} else if item, goalErr := s.goals.Get(request.Context(), sessionID); goalErr == nil {
		if item.State == goalstore.Paused {
			writeJSON(writer, http.StatusConflict, ExecuteResponse{SessionID: sessionID, Error: "goal is paused; resume or clear it before continuing"})
			return
		}
		if item.Active() {
			goalID = item.ID
			input.Message = item.ContinuationPrompt(input.Message)
		}
	} else if !errors.Is(goalErr, sql.ErrNoRows) {
		writeJSON(writer, http.StatusInternalServerError, ExecuteResponse{SessionID: sessionID, Error: goalErr.Error()})
		return
	}
	taskID, err := randomID("task_", 12)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, ExecuteResponse{Error: err.Error()})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now().UTC()
	current := &task{
		id: taskID, sessionID: sessionID, userMessage: userMessage, goalID: goalID,
		status: "queued", cancel: cancel,
		approval: make(chan approvalDecision, 1), done: make(chan struct{}),
		createdAt: now, updatedAt: now, store: s.taskStore, logger: s.logger,
	}
	if err := s.taskStore.save(request.Context(), current.record()); err != nil {
		cancel()
		writeJSON(writer, http.StatusInternalServerError, ExecuteResponse{Error: "persist task: " + err.Error()})
		return
	}
	s.mu.Lock()
	s.tasks[taskID] = current
	s.mu.Unlock()
	if err := s.queue.Enqueue(&queuedTask{ctx: ctx, task: current, input: input, images: images}); err != nil {
		current.finish(false, "", err)
		current.closeDone()
		writeJSON(writer, http.StatusTooManyRequests, ExecuteResponse{TaskID: taskID, SessionID: sessionID, Error: err.Error()})
		return
	}

	if input.Async {
		writeJSON(writer, http.StatusAccepted, ExecuteResponse{Success: true, TaskID: taskID, SessionID: sessionID})
		return
	}
	select {
	case <-request.Context().Done():
		s.queue.Cancel(taskID)
		writeJSON(writer, http.StatusRequestTimeout, ExecuteResponse{TaskID: taskID, SessionID: sessionID, Error: request.Context().Err().Error()})
	case <-current.done:
		status := current.snapshot()
		writeJSON(writer, http.StatusOK, ExecuteResponse{
			Success: status.Success, TaskID: taskID, SessionID: sessionID,
			Result: status.Result, Messages: status.Messages, Error: status.Error,
		})
	}
}

func (s *Server) runTask(ctx context.Context, current *task, input ExecuteRequest, images []string) {
	view, err := s.sessions.BuildPromptWithImages(ctx, current.sessionID, input.Message, images)
	if err != nil {
		current.finish(false, "", err)
		return
	}
	persistedTurn, err := s.sessions.BeginTurn(ctx, current.sessionID, "")
	if err != nil {
		current.finish(false, "", err)
		return
	}
	if err := persistedTurn.AddUser(current.userMessage, images); err != nil {
		current.finish(false, "", err)
		return
	}
	if view.Info.Compacted {
		current.addMessage(fmt.Sprintf("session compacted: dropped=%d bytes=%d->%d trigger=%s llm_summary=%t",
			view.Info.DroppedMessages, view.Info.BytesBefore, view.Info.BytesAfter, view.Info.TriggeredBy, view.Info.LLMSummaryUsed))
	}

	var result domain.RunMetrics
	var report string
	var runErr error
	var persistErr error
	var persistMu sync.Mutex
	recordPersistErr := func(err error) {
		if err == nil {
			return
		}
		persistMu.Lock()
		if persistErr == nil {
			persistErr = err
		}
		persistMu.Unlock()
	}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			minRecent := 1
			if attempt > 1 {
				minRecent = 0
			}
			view, err = s.sessions.ForcePromptWithImages(ctx, current.sessionID, input.Message, images, minRecent)
			if err != nil {
				runErr = err
				break
			}
			current.addMessage(fmt.Sprintf("context recovery %d/2: dropped=%d", attempt, view.Info.DroppedMessages))
		}
		var activeRunID string
		runtime, err := app.New(app.Config{
			DataDir: s.config.DataDir, SessionID: current.sessionID, Provider: s.config.Provider, Model: s.config.Model, FallbackModels: s.config.FallbackModels,
			WorkDir: s.config.WorkDir,
			APIKey:  s.config.APIKey, BaseURL: s.config.BaseURL, MaxOutputTokens: s.config.MaxOutputTokens,
			MaxSteps: s.config.MaxSteps, MaxGoalTurns: s.config.MaxGoalTurns,
			TokenBudget: s.config.TokenBudget, MaxRecentObservations: s.config.MaxRecentObservations,
			ToolTimeout: s.config.ToolTimeout, ToolOutputBytes: s.config.ToolOutputBytes,
			SubAgents: s.subagents,
			Approval:  s.approvalHandler(ctx, current, input.AutoApprove),
			OnEvent: func(event domain.Event) {
				recordPersistErr(persistedTurn.AddRuntimeEvent(activeRunID, event))
				current.addMessage(event.Type)
			},
			OnStream: func(delta string) {
				current.addMessage("assistant_delta:" + delta)
			},
		})
		if err != nil {
			runErr = err
			break
		}
		activeRunID = runtime.RunID
		recordPersistErr(persistedTurn.BindRunID(runtime.RunID))
		var agentResult agent.Result
		if input.ResumePlanID != "" {
			agentResult, err = runtime.Agent.ResumePlanWithSession(ctx, input.ResumePlanID, images, view.NativeHistory())
		} else {
			agentResult, err = runtime.Agent.RunWithSession(ctx, input.Message, images, view.NativeHistory())
		}
		_ = runtime.Close()
		result = agentResult.Metrics
		recordPersistErr(persistedTurn.MergeMetrics(agentResult.Metrics))
		persistMu.Lock()
		stagingErr := persistErr
		persistMu.Unlock()
		if err == nil && stagingErr != nil {
			err = stagingErr
		}
		if err == nil && agentResult.Status == "completed" {
			result = agentResult.Metrics
			report = agentResult.Report
			runErr = nil
			break
		}
		if err == nil {
			err = fmt.Errorf("agent stopped: %s", agentResult.Reason)
		}
		runErr = err
		if !isContextLengthError(err) || attempt == 2 {
			break
		}
	}
	if runErr != nil {
		status := turnStatusForError(ctx, runErr)
		if commitErr := commitServerTurn(ctx, persistedTurn, status, runErr.Error()); commitErr != nil {
			runErr = errors.Join(runErr, commitErr)
		}
		if current.goalID != "" {
			_, _ = s.goals.RecordProgress(context.Background(), current.sessionID, result.TotalTokens, 0, runErr.Error())
		}
		current.finish(false, "", runErr)
		return
	}
	if err := persistedTurn.AddAssistantText(report); err != nil {
		current.finish(false, "", err)
		return
	}
	if err := commitServerTurn(ctx, persistedTurn, session.TurnCompleted, ""); err != nil {
		current.finish(false, "", err)
		return
	}
	if current.goalID != "" {
		if _, err := s.goals.Achieve(ctx, current.sessionID, result.TotalTokens, 1, "agent completed and evaluator passed"); err != nil {
			current.finish(false, "", err)
			return
		}
	}
	current.finish(true, report, nil)
}

func commitServerTurn(ctx context.Context, turn *session.Turn, status session.TurnStatus, failure string) error {
	commitCtx := ctx
	if commitCtx == nil || commitCtx.Err() != nil {
		var cancel context.CancelFunc
		commitCtx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
	}
	return turn.Commit(commitCtx, status, failure)
}

func turnStatusForError(ctx context.Context, err error) session.TurnStatus {
	if errors.Is(err, context.DeadlineExceeded) || ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return session.TurnTimedOut
	}
	if errors.Is(err, context.Canceled) || ctx != nil && errors.Is(ctx.Err(), context.Canceled) {
		return session.TurnCancelled
	}
	return session.TurnFailed
}

func (s *Server) handleGoalStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	item, err := s.goals.Get(request.Context(), request.URL.Query().Get("session_id"))
	if err != nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, item)
}

func (s *Server) handleGoalAction(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		SessionID  string `json:"session_id"`
		Action     string `json:"action"`
		Objective  string `json:"objective"`
		AutoResume bool   `json:"auto_resume"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if strings.TrimSpace(payload.SessionID) == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}
	var item goalstore.Goal
	var err error
	switch strings.ToLower(strings.TrimSpace(payload.Action)) {
	case "set":
		if _, err = s.sessions.Ensure(request.Context(), payload.SessionID); err == nil {
			item, err = s.goals.Set(request.Context(), payload.SessionID, payload.Objective)
		}
	case "pause":
		item, err = s.goals.Pause(request.Context(), payload.SessionID)
	case "resume":
		item, err = s.goals.Resume(request.Context(), payload.SessionID, payload.AutoResume)
	case "clear":
		err = s.goals.Clear(request.Context(), payload.SessionID)
	default:
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "action must be set, pause, resume, or clear"})
		return
	}
	if err != nil {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if strings.EqualFold(payload.Action, "clear") {
		writeJSON(writer, http.StatusOK, map[string]any{"success": true})
		return
	}
	writeJSON(writer, http.StatusOK, item)
}

func (s *Server) approvalHandler(ctx context.Context, current *task, autoApprove bool) tools.ApprovalFunc {
	return func(_ context.Context, request tools.ApprovalRequest) (bool, error) {
		if autoApprove || current.remainingApproved() {
			return true, nil
		}
		toolID, err := randomID("tool_", 8)
		if err != nil {
			return false, err
		}
		params, _ := json.Marshal(request.Args)
		current.setPending(&PendingApproval{
			ToolID: toolID, ToolName: request.Tool, AgentName: "your-agent", ParamsPreview: truncate(string(params), 1000),
		})
		defer current.setPending(nil)
		for {
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case decision := <-current.approval:
				if decision.toolID == toolID {
					return decision.approved, nil
				}
			}
		}
	}
}

func (s *Server) handleStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	current := s.task(request.URL.Query().Get("task_id"))
	if current == nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	writeJSON(writer, http.StatusOK, current.snapshot())
}

func (s *Server) handleEvents(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	current := s.task(request.URL.Query().Get("task_id"))
	if current == nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "streaming is unavailable"})
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.Header().Set("Connection", "keep-alive")
	cursor := streamCursor(request)
	_, _ = io.WriteString(writer, "retry: 1000\n\n")
	flusher.Flush()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		snapshot := current.snapshot()
		for cursor < len(snapshot.Messages) {
			if !writeSSE(writer, cursor+1, "message", map[string]any{"type": "message", "index": cursor, "message": snapshot.Messages[cursor]}) {
				return
			}
			cursor++
		}
		if !writeSSE(writer, 0, "status", map[string]any{"type": "status", "status": snapshot}) {
			return
		}
		flusher.Flush()
		if snapshot.Complete {
			return
		}
		select {
		case <-request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) handleApprove(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		TaskID     string `json:"task_id"`
		ToolID     string `json:"tool_id"`
		Approved   bool   `json:"approved"`
		ApproveAll bool   `json:"approve_all"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	current := s.task(payload.TaskID)
	if current == nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if current.acceptApproval(payload.ToolID, payload.Approved, payload.ApproveAll) {
		writeJSON(writer, http.StatusOK, map[string]any{"success": true})
		return
	}
	writeJSON(writer, http.StatusConflict, map[string]string{"error": "task is not waiting for this approval"})
}

func (s *Server) handleCancel(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	current := s.task(request.URL.Query().Get("task_id"))
	if current == nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if s.queue != nil {
		s.queue.Cancel(current.id)
	} else {
		current.cancel()
	}
	writeJSON(writer, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) handleSessionStatus(writer http.ResponseWriter, request *http.Request) {
	status, err := s.sessions.Status(request.Context(), request.URL.Query().Get("session_id"))
	if err != nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, status)
}

func (s *Server) handlePlanLatest(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	item, err := s.plans.Latest(request.Context(), request.URL.Query().Get("session_id"))
	if err != nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, item)
}

func (s *Server) handlePlanAccept(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var payload struct {
		PlanID   string `json:"plan_id"`
		StepID   string `json:"step_id"`
		Evidence string `json:"evidence"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	item, err := (planning.Scheduler{Store: s.plans}).Accept(request.Context(), payload.PlanID, payload.StepID, payload.Evidence)
	if err != nil {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, item)
}

func (s *Server) task(id string) *task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks[strings.TrimSpace(id)]
}

func (t *task) snapshot() StatusResponse {
	t.mu.RLock()
	defer t.mu.RUnlock()
	messages := append([]string(nil), t.messages...)
	var pending *PendingApproval
	if t.pendingApproval != nil {
		copy := *t.pendingApproval
		pending = &copy
	}
	return StatusResponse{
		TaskID: t.id, SessionID: t.sessionID, UserMessage: t.userMessage, GoalID: t.goalID,
		Status: t.status, Messages: messages, Complete: t.complete,
		Success: t.success, Error: t.err, Result: t.result, PendingApproval: pending,
		CreatedAt: t.createdAt, UpdatedAt: t.updatedAt,
	}
}

func (t *task) markRunning() {
	t.mu.Lock()
	if !t.complete {
		t.status = "running"
		t.updatedAt = time.Now().UTC()
	}
	t.mu.Unlock()
	t.persist()
}

func (t *task) closeDone() {
	t.doneOnce.Do(func() { close(t.done) })
}

func (t *task) addMessage(message string) {
	t.mu.Lock()
	t.messages = append(t.messages, message)
	t.updatedAt = time.Now().UTC()
	t.mu.Unlock()
	if !strings.HasPrefix(message, "assistant_delta:") {
		t.persist()
	}
}

func (t *task) setPending(pending *PendingApproval) {
	t.mu.Lock()
	t.pendingApproval = pending
	if pending != nil {
		t.status = "pending_approval"
	} else if !t.complete {
		t.status = "running"
	}
	t.updatedAt = time.Now().UTC()
	t.mu.Unlock()
	t.persist()
}

func (t *task) acceptApproval(toolID string, approved, approveAll bool) bool {
	t.mu.Lock()
	if t.complete || t.pendingApproval == nil || t.pendingApproval.ToolID != strings.TrimSpace(toolID) {
		t.mu.Unlock()
		return false
	}
	if approved && approveAll {
		t.approveRemaining = true
	}
	t.updatedAt = time.Now().UTC()
	accepted := false
	select {
	case t.approval <- approvalDecision{toolID: toolID, approved: approved}:
		accepted = true
	default:
	}
	t.mu.Unlock()
	if accepted {
		t.persist()
	}
	return accepted
}

func (t *task) remainingApproved() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.approveRemaining
}

func (t *task) finish(success bool, result string, err error) {
	t.mu.Lock()
	t.complete = true
	t.success = success
	t.result = result
	t.pendingApproval = nil
	if err != nil {
		t.err = err.Error()
		if errors.Is(err, context.Canceled) {
			t.status = "canceled"
		} else {
			t.status = "failed"
		}
	} else {
		t.status = "completed"
	}
	t.updatedAt = time.Now().UTC()
	t.mu.Unlock()
	t.persist()
}

func (t *task) record() taskRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.recordLocked()
}

func (t *task) recordLocked() taskRecord {
	messages := append([]string(nil), t.messages...)
	var pending *PendingApproval
	if t.pendingApproval != nil {
		copy := *t.pendingApproval
		pending = &copy
	}
	return taskRecord{
		ID: t.id, SessionID: t.sessionID, UserMessage: t.userMessage, GoalID: t.goalID,
		Status: t.status, Result: t.result, Error: t.err, Messages: messages, PendingApproval: pending,
		ApproveRemaining: t.approveRemaining, Complete: t.complete, Success: t.success,
		CreatedAt: t.createdAt, UpdatedAt: t.updatedAt,
	}
}

func (t *task) persist() {
	if t.store == nil {
		return
	}
	if err := t.store.save(context.Background(), t.record()); err != nil && t.logger != nil {
		t.logger.Printf("persist task %s: %v", t.id, err)
	}
}

func taskFromRecord(record taskRecord, store *taskStore, logger *log.Logger) *task {
	done := make(chan struct{})
	close(done)
	return &task{
		id: record.ID, sessionID: record.SessionID, userMessage: record.UserMessage, goalID: record.GoalID,
		status: record.Status, result: record.Result, err: record.Error, messages: append([]string(nil), record.Messages...),
		pendingApproval: record.PendingApproval, approveRemaining: record.ApproveRemaining,
		approval: make(chan approvalDecision, 1), cancel: func() {}, done: done,
		complete: record.Complete, success: record.Success, createdAt: record.CreatedAt, updatedAt: record.UpdatedAt,
		store: store, logger: logger,
	}
}

func isContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{"context length", "context window", "maximum context", "too many tokens", "input is too long"} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func randomID(prefix string, bytesCount int) (string, error) {
	data := make([]byte, bytesCount)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(data), nil
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeSSE(writer io.Writer, id int, event string, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	if id > 0 {
		if _, err = fmt.Fprintf(writer, "id: %d\n", id); err != nil {
			return false
		}
	}
	if strings.TrimSpace(event) != "" {
		if _, err = fmt.Fprintf(writer, "event: %s\n", event); err != nil {
			return false
		}
	}
	_, err = fmt.Fprintf(writer, "data: %s\n\n", data)
	return err == nil
}

func streamCursor(request *http.Request) int {
	value := strings.TrimSpace(request.URL.Query().Get("after"))
	if value == "" {
		value = strings.TrimSpace(request.Header.Get("Last-Event-ID"))
	}
	cursor, err := strconv.Atoi(value)
	if err != nil || cursor < 0 {
		return 0
	}
	return cursor
}

func limitBody(next http.Handler, bytes int64) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, bytes)
		next.ServeHTTP(writer, request)
	})
}
