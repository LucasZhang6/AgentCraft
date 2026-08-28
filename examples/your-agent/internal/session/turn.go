package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

type TurnStatus string

const (
	TurnCompleted       TurnStatus = "completed"
	TurnPaused          TurnStatus = "paused"
	TurnWaitingForInput TurnStatus = "waiting_for_input"
	TurnFailed          TurnStatus = "failed"
	TurnCancelled       TurnStatus = "cancelled"
	TurnTimedOut        TurnStatus = "timed_out"
)

type TurnEventKind string

const (
	TurnEventMessage TurnEventKind = "message"
	TurnEventRuntime TurnEventKind = "runtime_event"
	TurnEventMetrics TurnEventKind = "turn_metrics"
	TurnEventStatus  TurnEventKind = "turn_status"
)

type TurnRecord struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"sessionId"`
	RunID       string     `json:"runId,omitempty"`
	Status      TurnStatus `json:"status"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	CommittedAt time.Time  `json:"committedAt"`
}

type CanonicalEvent struct {
	ID         int64                 `json:"id"`
	SessionID  string                `json:"sessionId"`
	TurnID     string                `json:"turnId"`
	Sequence   int64                 `json:"sequence"`
	EventIndex int                   `json:"eventIndex"`
	Kind       TurnEventKind         `json:"kind"`
	Role       string                `json:"role,omitempty"`
	Content    string                `json:"content,omitempty"`
	Blocks     []domain.ContentBlock `json:"blocks,omitempty"`
	Payload    json.RawMessage       `json:"payload,omitempty"`
	ToolCallID string                `json:"toolCallId,omitempty"`
	CreatedAt  time.Time             `json:"createdAt"`
}

type pendingTurnEvent struct {
	kind       TurnEventKind
	role       string
	content    string
	blocks     []domain.ContentBlock
	runID      string
	eventType  string
	payload    json.RawMessage
	toolCallID string
	createdAt  time.Time
}

// Turn buffers a complete logical user turn. Nothing is written until Commit,
// so messages, tool protocol, status, runtime events, and metrics share one
// SQLite transaction and one recovery boundary.
type Turn struct {
	mu        sync.Mutex
	store     *Store
	id        string
	sessionID string
	runID     string
	startedAt time.Time
	events    []pendingTurnEvent
	metrics   *domain.RunMetrics
	done      bool
}

func (s *Store) BeginTurn(ctx context.Context, sessionID, runID string) (*Turn, error) {
	sessionID, err := s.Ensure(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	id, err := newTurnID()
	if err != nil {
		return nil, err
	}
	return &Turn{store: s, id: id, sessionID: sessionID, runID: strings.TrimSpace(runID), startedAt: s.now().UTC()}, nil
}

func (t *Turn) ID() string {
	if t == nil {
		return ""
	}
	return t.id
}

// BindRunID records the first runtime that executed this logical turn. Retry
// runtimes remain available on their individual runtime events.
func (t *Turn) BindRunID(runID string) error {
	if t == nil || strings.TrimSpace(runID) == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return errors.New("session: turn is already finalized")
	}
	if t.runID == "" {
		t.runID = strings.TrimSpace(runID)
	}
	return nil
}

func (t *Turn) AddUser(text string, images []string) error {
	blocks := make([]domain.ContentBlock, 0, len(images)+1)
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, domain.ContentBlock{Type: domain.BlockText, Text: text})
	}
	for _, image := range images {
		if strings.TrimSpace(image) != "" {
			blocks = append(blocks, domain.ContentBlock{Type: domain.BlockImage, ImageURL: image})
		}
	}
	return t.addMessage("user", blocks)
}

func (t *Turn) AddAssistantText(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return t.addMessage("assistant", []domain.ContentBlock{{Type: domain.BlockText, Text: text}})
}

func (t *Turn) AddAssistantBlocks(blocks []domain.ContentBlock) error {
	return t.addMessage("assistant_blocks", blocks)
}

func (t *Turn) AddToolResults(blocks []domain.ContentBlock) error {
	return t.addMessage("tool_results", blocks)
}

func (t *Turn) addMessage(role string, blocks []domain.ContentBlock) error {
	if t == nil || len(blocks) == 0 {
		return nil
	}
	if err := validateMessageBlocks(role, blocks); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return errors.New("session: turn is already finalized")
	}
	copyBlocks := cloneBlocks(blocks)
	t.events = append(t.events, pendingTurnEvent{
		kind: TurnEventMessage, role: role, content: displayText(copyBlocks), blocks: copyBlocks,
		toolCallID: firstToolCallID(copyBlocks), createdAt: t.store.now().UTC(),
	})
	return nil
}

func (t *Turn) AddRuntimeEvent(runID string, event domain.Event) error {
	if t == nil {
		return nil
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("encode runtime event %s: %w", event.Type, err)
	}
	createdAt := event.Timestamp
	if createdAt.IsZero() {
		createdAt = t.store.now().UTC()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return errors.New("session: turn is already finalized")
	}
	t.events = append(t.events, pendingTurnEvent{
		kind: TurnEventRuntime, runID: strings.TrimSpace(runID), eventType: strings.TrimSpace(event.Type),
		payload: append(json.RawMessage(nil), payload...), createdAt: createdAt.UTC(),
	})
	derived, err := structuredMessagesForRuntimeEvent(event.Type, payload, createdAt.UTC())
	if err != nil {
		return err
	}
	t.events = append(t.events, derived...)
	return nil
}

func (t *Turn) SetMetrics(metrics domain.RunMetrics) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return errors.New("session: turn is already finalized")
	}
	copyMetrics := metrics
	t.metrics = &copyMetrics
	return nil
}

func (t *Turn) MergeMetrics(metrics domain.RunMetrics) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done {
		return errors.New("session: turn is already finalized")
	}
	if t.metrics == nil {
		copyMetrics := metrics
		t.metrics = &copyMetrics
		return nil
	}
	mergeRunMetrics(t.metrics, metrics)
	return nil
}

func (t *Turn) Abort() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.done = true
	t.events = nil
	t.metrics = nil
	t.mu.Unlock()
}

func (t *Turn) Commit(ctx context.Context, status TurnStatus, failure string) error {
	return t.commit(ctx, status, failure, false)
}

// CommitRetainingMessages keeps a failed turn in resume-compatible history.
// Callers must use it only when every staged tool call has a matching result.
func (t *Turn) CommitRetainingMessages(ctx context.Context, failure string) error {
	return t.commit(ctx, TurnFailed, failure, true)
}

func (t *Turn) commit(ctx context.Context, status TurnStatus, failure string, retainMessages bool) error {
	if t == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !validTurnStatus(status) {
		return fmt.Errorf("session: invalid turn status %q", status)
	}
	t.mu.Lock()
	if t.done {
		t.mu.Unlock()
		return errors.New("session: turn is already finalized")
	}
	t.done = true
	events := append([]pendingTurnEvent(nil), t.events...)
	var metrics *domain.RunMetrics
	if t.metrics != nil {
		copyMetrics := *t.metrics
		metrics = &copyMetrics
	}
	t.mu.Unlock()

	err := t.store.commitTurn(ctx, t.id, t.sessionID, t.runID, t.startedAt, status, strings.TrimSpace(failure), events, metrics, retainMessages)
	if err != nil {
		t.mu.Lock()
		t.done = false
		t.mu.Unlock()
		return err
	}
	t.store.maybePrewarmSummary(context.Background(), t.sessionID)
	return nil
}

func (s *Store) commitTurn(
	ctx context.Context,
	turnID, sessionID, runID string,
	startedAt time.Time,
	status TurnStatus,
	failure string,
	events []pendingTurnEvent,
	metrics *domain.RunMetrics,
	retainMessages bool,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin canonical session turn: %w", err)
	}
	defer tx.Rollback()

	var existing string
	err = tx.QueryRowContext(ctx, `SELECT session_id FROM session_turns WHERE id = ?`, turnID).Scan(&existing)
	if err == nil {
		if existing != sessionID {
			return fmt.Errorf("session: turn %s belongs to another session", turnID)
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	committedAt := s.now().UTC()
	if startedAt.IsZero() {
		startedAt = committedAt
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO session_turns(id, session_id, run_id, status, error, started_at, committed_at)
VALUES(?, ?, ?, ?, ?, ?, ?)`, turnID, sessionID, runID, status, failure,
		startedAt.Format(time.RFC3339Nano), committedAt.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("insert session turn: %w", err)
	}
	var nextSequence int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM session_turn_events WHERE session_id = ?`, sessionID,
	).Scan(&nextSequence); err != nil {
		return fmt.Errorf("allocate session event sequence: %w", err)
	}
	firstUserText := ""
	materializeMessages := retainMessages || status != TurnFailed && status != TurnCancelled && status != TurnTimedOut
	for index, event := range events {
		createdAt := event.createdAt
		if createdAt.IsZero() {
			createdAt = committedAt
		}
		blocksJSON := ""
		payloadJSON := string(event.payload)
		if payloadJSON == "" {
			payloadJSON = "{}"
		}
		if event.kind == TurnEventMessage {
			encoded, marshalErr := json.Marshal(event.blocks)
			if marshalErr != nil {
				return fmt.Errorf("encode session message blocks: %w", marshalErr)
			}
			blocksJSON = string(encoded)
			payloadJSON = blocksJSON
			if materializeMessages {
				insert, execErr := tx.ExecContext(ctx, `
INSERT INTO session_messages(session_id, turn_id, sequence, role, content, blocks_json, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?)`, sessionID, turnID, nextSequence, event.role, event.content, blocksJSON, createdAt.Format(time.RFC3339Nano))
				if execErr != nil {
					return fmt.Errorf("materialize session message: %w", execErr)
				}
				if _, idErr := insert.LastInsertId(); idErr != nil {
					return fmt.Errorf("read session message id: %w", idErr)
				}
			}
			if firstUserText == "" && event.role == "user" {
				firstUserText = event.content
			}
		} else if event.kind == TurnEventRuntime {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO session_events(session_id, run_id, event_type, payload_json, created_at)
VALUES(?, ?, ?, ?, ?)`, sessionID, event.runID, event.eventType, payloadJSON, createdAt.Format(time.RFC3339Nano)); err != nil {
				return fmt.Errorf("materialize runtime event: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO session_turn_events
(session_id, turn_id, sequence, event_index, kind, role, content, blocks_json, payload_json, tool_call_id, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, sessionID, turnID, nextSequence, index, event.kind,
			event.role, eventContent(event), blocksJSON, payloadJSON, event.toolCallID, createdAt.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("insert canonical session event: %w", err)
		}
		nextSequence++
	}

	eventIndex := len(events)
	lastInputTokens := 0
	if metrics != nil {
		normalizeSessionMetrics(metrics, startedAt, committedAt, status)
		lastInputTokens = max(metrics.InputTokens, 0)
		metricsJSON, marshalErr := json.Marshal(metrics)
		if marshalErr != nil {
			return fmt.Errorf("encode session turn metrics: %w", marshalErr)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO session_turn_metrics
(turn_id, session_id, status, metrics_json, started_at, completed_at, duration_ms, llm_calls,
 input_tokens, output_tokens, total_tokens, tool_calls, tool_failures, human_approval_requests,
 context_compactions, success)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, turnID, sessionID, status, string(metricsJSON),
			metrics.StartedAt.Format(time.RFC3339Nano), metrics.CompletedAt.Format(time.RFC3339Nano), metrics.DurationMS,
			metrics.LLMCalls, metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens, metrics.ToolCalls,
			metrics.ToolFailures, metrics.HumanApprovalRequests, metrics.ContextCompactions, metrics.Success); err != nil {
			return fmt.Errorf("insert session turn metrics: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO session_turn_events
(session_id, turn_id, sequence, event_index, kind, content, payload_json, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, sessionID, turnID, nextSequence, eventIndex, TurnEventMetrics,
			string(status), string(metricsJSON), committedAt.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("insert canonical metrics event: %w", err)
		}
		nextSequence++
		eventIndex++
	}
	statusPayload, _ := json.Marshal(map[string]string{"status": string(status), "error": failure})
	if _, err := tx.ExecContext(ctx, `
INSERT INTO session_turn_events
(session_id, turn_id, sequence, event_index, kind, content, payload_json, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, sessionID, turnID, nextSequence, eventIndex, TurnEventStatus,
		string(status), string(statusPayload), committedAt.Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("insert canonical status event: %w", err)
	}

	title := ""
	if materializeMessages {
		title = titleFromContent(firstUserText)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE sessions
SET title = CASE WHEN title = '' THEN ? ELSE title END,
    last_input_tokens = ?, updated_at = ?
WHERE id = ?`, title, lastInputTokens, committedAt.Format(time.RFC3339Nano), sessionID); err != nil {
		return fmt.Errorf("update committed session turn: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit canonical session turn: %w", err)
	}
	return nil
}

func (s *Store) Turns(ctx context.Context, sessionID string) ([]TurnRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, run_id, status, error, started_at, committed_at
FROM session_turns WHERE session_id = ? ORDER BY committed_at, id`, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []TurnRecord
	for rows.Next() {
		var item TurnRecord
		var started, committed string
		if err := rows.Scan(&item.ID, &item.SessionID, &item.RunID, &item.Status, &item.Error, &started, &committed); err != nil {
			return nil, err
		}
		item.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		item.CommittedAt, _ = time.Parse(time.RFC3339Nano, committed)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) MetricsForTurn(ctx context.Context, turnID string) (domain.RunMetrics, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx,
		`SELECT metrics_json FROM session_turn_metrics WHERE turn_id = ?`, strings.TrimSpace(turnID),
	).Scan(&raw); err != nil {
		return domain.RunMetrics{}, err
	}
	var result domain.RunMetrics
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return domain.RunMetrics{}, err
	}
	return result, nil
}

func (s *Store) CanonicalEvents(ctx context.Context, sessionID string) ([]CanonicalEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, turn_id, sequence, event_index, kind, role, content, blocks_json,
       payload_json, tool_call_id, created_at
FROM session_turn_events WHERE session_id = ? ORDER BY sequence`, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []CanonicalEvent
	for rows.Next() {
		var item CanonicalEvent
		var blocksJSON, payloadJSON, created string
		if err := rows.Scan(&item.ID, &item.SessionID, &item.TurnID, &item.Sequence, &item.EventIndex,
			&item.Kind, &item.Role, &item.Content, &blocksJSON, &payloadJSON, &item.ToolCallID, &created); err != nil {
			return nil, err
		}
		if blocksJSON != "" {
			_ = json.Unmarshal([]byte(blocksJSON), &item.Blocks)
		}
		item.Payload = json.RawMessage(payloadJSON)
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func structuredMessagesForRuntimeEvent(eventType string, payload json.RawMessage, createdAt time.Time) ([]pendingTurnEvent, error) {
	switch eventType {
	case "assistant_blocks":
		var value struct {
			Blocks []domain.ContentBlock `json:"blocks"`
		}
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, fmt.Errorf("decode assistant blocks event: %w", err)
		}
		if len(value.Blocks) == 0 {
			return nil, nil
		}
		if err := validateMessageBlocks("assistant_blocks", value.Blocks); err != nil {
			return nil, err
		}
		return []pendingTurnEvent{{
			kind: TurnEventMessage, role: "assistant_blocks", content: displayText(value.Blocks),
			blocks: cloneBlocks(value.Blocks), toolCallID: firstToolCallID(value.Blocks), createdAt: createdAt,
		}}, nil
	case "tool_call":
		var value struct {
			ToolCallID string          `json:"toolCallId"`
			Tool       string          `json:"tool"`
			Args       json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, fmt.Errorf("decode tool call event: %w", err)
		}
		if len(value.Args) == 0 || !json.Valid(value.Args) {
			value.Args = json.RawMessage(`{}`)
		}
		block := domain.ContentBlock{
			Type: domain.BlockToolCall, ToolCallID: value.ToolCallID, ToolName: value.Tool,
			Arguments: append(json.RawMessage(nil), value.Args...),
		}
		if err := validateMessageBlocks("assistant_blocks", []domain.ContentBlock{block}); err != nil {
			return nil, err
		}
		return []pendingTurnEvent{{
			kind: TurnEventMessage, role: "assistant_blocks", content: displayText([]domain.ContentBlock{block}),
			blocks: []domain.ContentBlock{block}, toolCallID: value.ToolCallID, createdAt: createdAt,
		}}, nil
	case "tool_results":
		var value struct {
			Blocks []domain.ContentBlock `json:"blocks"`
		}
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, fmt.Errorf("decode native tool results event: %w", err)
		}
		if len(value.Blocks) == 0 {
			return nil, nil
		}
		if err := validateMessageBlocks("tool_results", value.Blocks); err != nil {
			return nil, err
		}
		return []pendingTurnEvent{{
			kind: TurnEventMessage, role: "tool_results", content: displayText(value.Blocks),
			blocks: cloneBlocks(value.Blocks), toolCallID: firstToolCallID(value.Blocks), createdAt: createdAt,
		}}, nil
	case "react_user_message":
		var value struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, fmt.Errorf("decode ReAct continuation event: %w", err)
		}
		if strings.TrimSpace(value.Text) == "" {
			return nil, nil
		}
		blocks := []domain.ContentBlock{{Type: domain.BlockText, Text: value.Text}}
		return []pendingTurnEvent{{
			kind: TurnEventMessage, role: "user", content: value.Text,
			blocks: blocks, createdAt: createdAt,
		}}, nil
	case "tool_succeeded", "tool_failed":
		var value struct {
			Observation domain.Observation `json:"observation"`
		}
		if err := json.Unmarshal(payload, &value); err != nil {
			return nil, fmt.Errorf("decode tool result event: %w", err)
		}
		output := value.Observation.Error
		if value.Observation.OK {
			encoded, err := json.Marshal(value.Observation.Result)
			if err != nil {
				return nil, fmt.Errorf("encode tool result output: %w", err)
			}
			output = string(encoded)
		}
		block := domain.ContentBlock{
			Type: domain.BlockToolResult, ToolCallID: value.Observation.ToolCallID,
			Output: output, IsError: !value.Observation.OK,
		}
		if err := validateMessageBlocks("tool_results", []domain.ContentBlock{block}); err != nil {
			return nil, err
		}
		return []pendingTurnEvent{{
			kind: TurnEventMessage, role: "tool_results", content: displayText([]domain.ContentBlock{block}),
			blocks: []domain.ContentBlock{block}, toolCallID: value.Observation.ToolCallID, createdAt: createdAt,
		}}, nil
	default:
		return nil, nil
	}
}

func validateMessageBlocks(role string, blocks []domain.ContentBlock) error {
	role = strings.TrimSpace(role)
	if role == "" {
		return errors.New("session: message role is required")
	}
	for _, block := range blocks {
		switch block.Type {
		case domain.BlockText:
		case domain.BlockImage:
			if strings.TrimSpace(block.ImageURL) == "" {
				return errors.New("session: image block is missing image URL")
			}
		case domain.BlockReasoning:
			if len(block.Raw) > 0 && !json.Valid(block.Raw) {
				return errors.New("session: reasoning block contains invalid raw JSON")
			}
		case domain.BlockToolCall:
			if strings.TrimSpace(block.ToolCallID) == "" || strings.TrimSpace(block.ToolName) == "" {
				return errors.New("session: tool call block requires id and name")
			}
			if len(block.Arguments) > 0 && !json.Valid(block.Arguments) {
				return fmt.Errorf("session: tool call %s contains invalid arguments", block.ToolCallID)
			}
		case domain.BlockToolResult:
			if strings.TrimSpace(block.ToolCallID) == "" {
				return errors.New("session: tool result block requires tool call id")
			}
		default:
			return fmt.Errorf("session: unsupported content block type %q", block.Type)
		}
	}
	return nil
}

func firstToolCallID(blocks []domain.ContentBlock) string {
	for _, block := range blocks {
		if block.ToolCallID != "" {
			return block.ToolCallID
		}
	}
	return ""
}

func eventContent(event pendingTurnEvent) string {
	if event.kind == TurnEventRuntime {
		return event.eventType
	}
	return event.content
}

func normalizeSessionMetrics(metrics *domain.RunMetrics, startedAt, completedAt time.Time, status TurnStatus) {
	if metrics.StartedAt.IsZero() {
		metrics.StartedAt = startedAt
	}
	if metrics.CompletedAt.IsZero() {
		metrics.CompletedAt = completedAt
	}
	if metrics.DurationMS <= 0 {
		metrics.DurationMS = metrics.CompletedAt.Sub(metrics.StartedAt).Milliseconds()
	}
	metrics.Success = status == TurnCompleted
}

func mergeRunMetrics(target *domain.RunMetrics, next domain.RunMetrics) {
	if target.StartedAt.IsZero() || !next.StartedAt.IsZero() && next.StartedAt.Before(target.StartedAt) {
		target.StartedAt = next.StartedAt
	}
	if next.CompletedAt.After(target.CompletedAt) {
		target.CompletedAt = next.CompletedAt
	}
	target.DurationMS += next.DurationMS
	target.LLMCalls += next.LLMCalls
	target.InputTokens += next.InputTokens
	target.OutputTokens += next.OutputTokens
	target.TotalTokens += next.TotalTokens
	target.CacheReadInputTokens += next.CacheReadInputTokens
	target.CacheCreationInputTokens += next.CacheCreationInputTokens
	target.ToolCalls += next.ToolCalls
	target.ToolFailures += next.ToolFailures
	target.ToolDurationMS += next.ToolDurationMS
	target.HumanApprovalRequests += next.HumanApprovalRequests
	target.ContextCompactions += next.ContextCompactions
	target.GoalTurns += next.GoalTurns
	target.Success = next.Success
}

func validTurnStatus(status TurnStatus) bool {
	switch status {
	case TurnCompleted, TurnPaused, TurnWaitingForInput, TurnFailed, TurnCancelled, TurnTimedOut:
		return true
	default:
		return false
	}
}

func titleFromContent(content string) string {
	content = strings.Join(strings.Fields(content), " ")
	if content == "" {
		return "New conversation"
	}
	runes := []rune(content)
	if len(runes) > 50 {
		return string(runes[:47]) + "..."
	}
	return content
}

func newTurnID() (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate session turn id: %w", err)
	}
	return "turn_" + hex.EncodeToString(random), nil
}
