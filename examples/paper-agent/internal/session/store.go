package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
	_ "github.com/mattn/go-sqlite3"
)

const (
	DefaultTriggerBytes      = 400_000
	DefaultTriggerTokens     = 120_000
	DefaultMinRecentTurns    = 4
	DefaultSummaryTimeout    = 12 * time.Second
	DefaultSummaryPrewarm    = 0.75
	maxPromptMessageBytes    = 64 * 1024
	compactedMessageHeadTail = 4 * 1024
)

var URLPattern = regexp.MustCompile(`https?://[^\s)\]>]+`)

type Config struct {
	TriggerBytes        int
	TriggerTokens       int
	MinRecentTurns      int
	Summarizer          Summarizer
	SummaryTimeout      time.Duration
	SummaryPrewarmRatio float64
}

type Store struct {
	db         *sql.DB
	config     Config
	now        func() time.Time
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	summaries  map[string]*summaryCache
	summarizer Summarizer
	wg         sync.WaitGroup
	closed     bool
}

type Message struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"sessionId"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

type Event struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"sessionId"`
	RunID     string          `json:"runId,omitempty"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
}

type Info struct {
	Compacted        bool   `json:"compacted"`
	Forced           bool   `json:"forced"`
	TriggeredBy      string `json:"triggeredBy,omitempty"`
	DroppedMessages  int    `json:"droppedMessages"`
	MicroCompactions int    `json:"microCompactions"`
	BytesBefore      int    `json:"bytesBefore"`
	BytesAfter       int    `json:"bytesAfter"`
	RetainedTurns    int    `json:"retainedTurns"`
	LLMSummaryUsed   bool   `json:"llmSummaryUsed"`
	SummaryState     string `json:"summaryState,omitempty"`
	SummaryCovered   int    `json:"summaryCoveredMessages,omitempty"`
}

type View struct {
	SessionID string    `json:"sessionId"`
	Prompt    string    `json:"prompt"`
	Messages  []Message `json:"messages"`
	Info      Info      `json:"info"`
}

type Status struct {
	SessionID           string    `json:"sessionId"`
	Title               string    `json:"title"`
	ForkedFromID        string    `json:"forkedFromId,omitempty"`
	MessageCount        int       `json:"messageCount"`
	EventCount          int       `json:"eventCount"`
	LastInputTokens     int       `json:"lastInputTokens"`
	SummaryCalls        int       `json:"summaryCalls"`
	SummaryInputTokens  int       `json:"summaryInputTokens"`
	SummaryOutputTokens int       `json:"summaryOutputTokens"`
	SummaryLatencyMS    int64     `json:"summaryLatencyMs"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	SummaryState        string    `json:"summaryState"`
	SummaryCovered      int       `json:"summaryCoveredMessages"`
}

func NewStore(path string, config Config) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("session database path is required")
	}
	if config.TriggerBytes <= 0 {
		config.TriggerBytes = DefaultTriggerBytes
	}
	if config.TriggerTokens <= 0 {
		config.TriggerTokens = DefaultTriggerTokens
	}
	if config.MinRecentTurns < 0 {
		config.MinRecentTurns = DefaultMinRecentTurns
	}
	if config.MinRecentTurns == 0 {
		config.MinRecentTurns = DefaultMinRecentTurns
	}
	if config.SummaryTimeout <= 0 {
		config.SummaryTimeout = DefaultSummaryTimeout
	}
	if config.SummaryPrewarmRatio <= 0 || config.SummaryPrewarmRatio >= 1 {
		config.SummaryPrewarmRatio = DefaultSummaryPrewarm
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on"}).String()
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open session database: %w", err)
	}
	database.SetMaxOpenConns(1)
	storeCtx, cancel := context.WithCancel(context.Background())
	store := &Store{
		db: database, config: config, now: time.Now, ctx: storeCtx, cancel: cancel,
		summaries: make(map[string]*summaryCache), summarizer: config.Summarizer,
	}
	if err := store.init(); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) init() error {
	schema := `
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    forked_from_id TEXT NOT NULL DEFAULT '',
    last_input_tokens INTEGER NOT NULL DEFAULT 0,
    summary_calls INTEGER NOT NULL DEFAULT 0,
    summary_input_tokens INTEGER NOT NULL DEFAULT 0,
    summary_output_tokens INTEGER NOT NULL DEFAULT 0,
    summary_latency_ms INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_session_messages_session_id ON session_messages(session_id, id);`
	schema += `
CREATE TABLE IF NOT EXISTS session_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_session_events_session_id ON session_events(session_id, id);
CREATE INDEX IF NOT EXISTS idx_session_events_run_id ON session_events(run_id, id);`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("initialize session database: %w", err)
	}
	for name, definition := range map[string]string{
		"forked_from_id":        "TEXT NOT NULL DEFAULT ''",
		"summary_calls":         "INTEGER NOT NULL DEFAULT 0",
		"summary_input_tokens":  "INTEGER NOT NULL DEFAULT 0",
		"summary_output_tokens": "INTEGER NOT NULL DEFAULT 0",
		"summary_latency_ms":    "INTEGER NOT NULL DEFAULT 0",
	} {
		if err := s.ensureSessionColumn(name, definition); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureSessionColumn(name, definition string) error {
	rows, err := s.db.Query(`PRAGMA table_info(sessions)`)
	if err != nil {
		return fmt.Errorf("inspect session schema: %w", err)
	}
	found := false
	for rows.Next() {
		var cid int
		var column, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &column, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan session schema: %w", err)
		}
		if column == name {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate session schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close session schema rows: %w", err)
	}
	if found {
		return nil
	}
	if _, err := s.db.Exec(`ALTER TABLE sessions ADD COLUMN ` + name + ` ` + definition); err != nil {
		return fmt.Errorf("add session column %s: %w", name, err)
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
	return s.db.Close()
}

func (s *Store) Ensure(ctx context.Context, sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		generated, err := newSessionID()
		if err != nil {
			return "", err
		}
		sessionID = generated
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO sessions(id, created_at, updated_at) VALUES(?, ?, ?)
ON CONFLICT(id) DO NOTHING`, sessionID, now, now); err != nil {
		return "", fmt.Errorf("ensure session: %w", err)
	}
	return sessionID, nil
}

func (s *Store) AppendTurn(ctx context.Context, sessionID, userText, assistantText string, inputTokens int) error {
	sessionID, err := s.Ensure(ctx, sessionID)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session turn: %w", err)
	}
	defer tx.Rollback()
	now := s.now().UTC().Format(time.RFC3339Nano)
	for _, message := range []struct {
		role    string
		content string
	}{{"user", userText}, {"assistant", assistantText}} {
		if strings.TrimSpace(message.content) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO session_messages(session_id, role, content, created_at) VALUES(?, ?, ?, ?)`,
			sessionID, message.role, message.content, now,
		); err != nil {
			return fmt.Errorf("append session %s message: %w", message.role, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET last_input_tokens = ?, updated_at = ? WHERE id = ?`, max(inputTokens, 0), now, sessionID,
	); err != nil {
		return fmt.Errorf("update session usage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session turn: %w", err)
	}
	s.maybePrewarmSummary(ctx, sessionID)
	return nil
}

func (s *Store) AppendEvent(ctx context.Context, sessionID, runID string, event domain.Event) error {
	sessionID, err := s.Ensure(ctx, sessionID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("encode session event payload: %w", err)
	}
	created := event.Timestamp
	if created.IsZero() {
		created = s.now().UTC()
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO session_events(session_id, run_id, event_type, payload_json, created_at)
VALUES(?, ?, ?, ?, ?)`, sessionID, strings.TrimSpace(runID), strings.TrimSpace(event.Type), string(payload), created.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("append session event: %w", err)
	}
	return nil
}

func (s *Store) Events(ctx context.Context, sessionID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, run_id, event_type, payload_json, created_at FROM (
    SELECT id, session_id, run_id, event_type, payload_json, created_at
    FROM session_events WHERE session_id = ? ORDER BY id DESC LIMIT ?
) ORDER BY id`, strings.TrimSpace(sessionID), limit)
	if err != nil {
		return nil, fmt.Errorf("list session events: %w", err)
	}
	defer rows.Close()
	var result []Event
	for rows.Next() {
		var item Event
		var payload, created string
		if err := rows.Scan(&item.ID, &item.SessionID, &item.RunID, &item.Type, &payload, &created); err != nil {
			return nil, fmt.Errorf("scan session event: %w", err)
		}
		item.Payload = json.RawMessage(payload)
		item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) BuildPrompt(ctx context.Context, sessionID, current string) (View, error) {
	return s.buildPrompt(ctx, sessionID, current, false, s.config.MinRecentTurns)
}

func (s *Store) ForcePrompt(ctx context.Context, sessionID, current string, minRecentTurns int) (View, error) {
	if minRecentTurns < 0 {
		minRecentTurns = 0
	}
	return s.buildPrompt(ctx, sessionID, current, true, minRecentTurns)
}

func (s *Store) buildPrompt(ctx context.Context, sessionID, current string, force bool, minRecentTurns int) (View, error) {
	sessionID, err := s.Ensure(ctx, sessionID)
	if err != nil {
		return View{}, err
	}
	messages, err := s.Messages(ctx, sessionID)
	if err != nil {
		return View{}, err
	}
	messages = append(messages, Message{SessionID: sessionID, Role: "user", Content: current, CreatedAt: s.now().UTC()})
	bytesBefore := totalBytes(messages)
	lastTokens, err := s.lastInputTokens(ctx, sessionID)
	if err != nil {
		return View{}, err
	}
	triggeredBy := ""
	if bytesBefore > s.config.TriggerBytes {
		triggeredBy = "bytes"
	}
	if lastTokens > s.config.TriggerTokens {
		if triggeredBy != "" {
			triggeredBy += "+tokens"
		} else {
			triggeredBy = "tokens"
		}
	}
	if force {
		triggeredBy = "context_limit"
	}

	promptMessages := cloneMessages(messages)
	microCount := microCompact(promptMessages)
	info := Info{
		Forced: force, TriggeredBy: triggeredBy, BytesBefore: bytesBefore,
		MicroCompactions: microCount, RetainedTurns: countUserTurns(promptMessages),
	}
	if triggeredBy != "" {
		cutIndex := findCutIndex(promptMessages, minRecentTurns)
		if cutIndex > 0 {
			dropped := cloneMessages(promptMessages[:cutIndex])
			llmSummary, summaryState, summaryCovered, summaryGeneration := s.summaryFor(sessionID, cutIndex)
			content := fmt.Sprintf(
				"[Earlier session truncated for context length: %d earlier messages elided. Full history remains in the local session database.]\n\n<session_digest>\n%s\n</session_digest>",
				len(dropped), digest(dropped, 4000),
			)
			if llmSummary != "" {
				content += "\n\n<conversation_summary>\n" + sanitizeSummary(llmSummary) + "\n</conversation_summary>"
			}
			placeholder := Message{
				SessionID: sessionID,
				Role:      "user",
				Content:   content,
				CreatedAt: s.now().UTC(),
			}
			promptMessages = append([]Message{placeholder}, promptMessages[cutIndex:]...)
			info.Compacted = true
			info.DroppedMessages = len(dropped)
			info.RetainedTurns = countUserTurns(promptMessages) - 1
			info.LLMSummaryUsed = llmSummary != ""
			info.SummaryState = summaryState
			info.SummaryCovered = summaryCovered
			if llmSummary != "" {
				s.consumeSummary(sessionID, summaryGeneration)
			}
		}
	}
	if info.SummaryState == "" {
		info.SummaryState, info.SummaryCovered = s.summaryStatus(sessionID)
	}
	info.BytesAfter = totalBytes(promptMessages)
	prompt := renderPrompt(promptMessages)
	if events, eventErr := s.Events(ctx, sessionID, 80); eventErr == nil {
		if rendered := renderEvents(events, 32*1024); rendered != "" {
			prompt += "\n\n" + rendered
		}
	}
	return View{SessionID: sessionID, Prompt: prompt, Messages: promptMessages, Info: info}, nil
}

func (s *Store) Messages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, role, content, created_at
FROM session_messages WHERE session_id = ? ORDER BY id`, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		var message Message
		var created string
		if err := rows.Scan(&message.ID, &message.SessionID, &message.Role, &message.Content, &created); err != nil {
			return nil, fmt.Errorf("scan session message: %w", err)
		}
		message.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session messages: %w", err)
	}
	return messages, nil
}

func (s *Store) Status(ctx context.Context, sessionID string) (Status, error) {
	var status Status
	var created, updated string
	err := s.db.QueryRowContext(ctx, `
SELECT s.id, s.title, s.forked_from_id, s.last_input_tokens,
       s.summary_calls, s.summary_input_tokens, s.summary_output_tokens, s.summary_latency_ms,
	       s.created_at, s.updated_at, COUNT(m.id),
	       (SELECT COUNT(*) FROM session_events e WHERE e.session_id = s.id)
FROM sessions s LEFT JOIN session_messages m ON m.session_id = s.id
WHERE s.id = ? GROUP BY s.id`, strings.TrimSpace(sessionID)).Scan(
		&status.SessionID, &status.Title, &status.ForkedFromID, &status.LastInputTokens,
		&status.SummaryCalls, &status.SummaryInputTokens, &status.SummaryOutputTokens, &status.SummaryLatencyMS,
		&created, &updated, &status.MessageCount, &status.EventCount,
	)
	if err != nil {
		return Status{}, fmt.Errorf("read session status: %w", err)
	}
	status.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	status.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	status.SummaryState, status.SummaryCovered = s.summaryStatus(sessionID)
	return status, nil
}

func (s *Store) List(ctx context.Context, limit int) ([]Status, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT s.id, s.title, s.forked_from_id, s.last_input_tokens,
       s.summary_calls, s.summary_input_tokens, s.summary_output_tokens, s.summary_latency_ms,
	       s.created_at, s.updated_at, COUNT(m.id),
	       (SELECT COUNT(*) FROM session_events e WHERE e.session_id = s.id)
FROM sessions s LEFT JOIN session_messages m ON m.session_id = s.id
GROUP BY s.id ORDER BY s.updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()
	result := make([]Status, 0, limit)
	for rows.Next() {
		var status Status
		var created, updated string
		if err := rows.Scan(
			&status.SessionID, &status.Title, &status.ForkedFromID, &status.LastInputTokens,
			&status.SummaryCalls, &status.SummaryInputTokens, &status.SummaryOutputTokens, &status.SummaryLatencyMS,
			&created, &updated, &status.MessageCount, &status.EventCount,
		); err != nil {
			return nil, fmt.Errorf("scan session list: %w", err)
		}
		status.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		status.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		status.SummaryState, status.SummaryCovered = s.summaryStatus(status.SessionID)
		result = append(result, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return result, nil
}

func (s *Store) UpdateTitle(ctx context.Context, sessionID, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("session title is required")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`,
		title, s.now().UTC().Format(time.RFC3339Nano), strings.TrimSpace(sessionID),
	)
	if err != nil {
		return fmt.Errorf("update session title: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) Fork(ctx context.Context, sessionID, title string) (string, error) {
	sourceID := strings.TrimSpace(sessionID)
	if sourceID == "" {
		return "", errors.New("source session id is required")
	}
	newID, err := newSessionID()
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin session fork: %w", err)
	}
	defer tx.Rollback()
	var sourceTitle string
	var lastInputTokens int
	if err := tx.QueryRowContext(ctx,
		`SELECT title, last_input_tokens FROM sessions WHERE id = ?`, sourceID,
	).Scan(&sourceTitle, &lastInputTokens); err != nil {
		return "", fmt.Errorf("read source session: %w", err)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		if sourceTitle == "" {
			title = "Fork of " + sourceID
		} else {
			title = sourceTitle + " (fork)"
		}
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO sessions(id, title, forked_from_id, last_input_tokens, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?)`, newID, title, sourceID, lastInputTokens, now, now); err != nil {
		return "", fmt.Errorf("create forked session: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO session_messages(session_id, role, content, created_at)
SELECT ?, role, content, created_at FROM session_messages WHERE session_id = ? ORDER BY id`, newID, sourceID); err != nil {
		return "", fmt.Errorf("copy session messages: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO session_events(session_id, run_id, event_type, payload_json, created_at)
SELECT ?, run_id, event_type, payload_json, created_at FROM session_events WHERE session_id = ? ORDER BY id`, newID, sourceID); err != nil {
		return "", fmt.Errorf("copy session events: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit session fork: %w", err)
	}
	return newID, nil
}

func (s *Store) Clear(ctx context.Context, sessionID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, strings.TrimSpace(sessionID))
	if err != nil {
		return fmt.Errorf("clear session: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	s.invalidateSummary(sessionID)
	return nil
}

func (s *Store) lastInputTokens(ctx context.Context, sessionID string) (int, error) {
	var tokens int
	if err := s.db.QueryRowContext(ctx,
		`SELECT last_input_tokens FROM sessions WHERE id = ?`, sessionID,
	).Scan(&tokens); err != nil {
		return 0, fmt.Errorf("read session token usage: %w", err)
	}
	return tokens, nil
}

func totalBytes(messages []Message) int {
	total := 0
	for _, message := range messages {
		total += len(message.Content)
	}
	return total
}

func cloneMessages(messages []Message) []Message {
	result := make([]Message, len(messages))
	copy(result, messages)
	return result
}

func microCompact(messages []Message) int {
	compacted := 0
	seen := make(map[string]struct{})
	for index := range messages {
		message := &messages[index]
		if index < len(messages)-2 {
			key := message.Role + "\x00" + message.Content
			if _, exists := seen[key]; exists && len(message.Content) > 256 {
				message.Content = fmt.Sprintf("[Repeated %s message elided: %d bytes]", message.Role, len(message.Content))
				compacted++
				continue
			}
			seen[key] = struct{}{}
		}
		if index < len(messages)-2 && len(message.Content) > maxPromptMessageBytes {
			originalBytes := len(message.Content)
			head := truncateUTF8(message.Content, compactedMessageHeadTail)
			tailStart := max(len(message.Content)-compactedMessageHeadTail, 0)
			tail := message.Content[tailStart:]
			for !utf8.ValidString(tail) && tailStart < len(message.Content) {
				tailStart++
				tail = message.Content[tailStart:]
			}
			message.Content = fmt.Sprintf("%s\n\n[Middle of old %s message elided: %d bytes total]\n\n%s", head, message.Role, originalBytes, tail)
			compacted++
		}
	}
	return compacted
}

func findCutIndex(messages []Message, minRecentTurns int) int {
	seen := 0
	for index := len(messages) - 1; index > 0; index-- {
		if messages[index].Role != "user" {
			continue
		}
		seen++
		if seen >= minRecentTurns+1 {
			return index
		}
	}
	return -1
}

func countUserTurns(messages []Message) int {
	count := 0
	for _, message := range messages {
		if message.Role == "user" {
			count++
		}
	}
	return count
}

func digest(messages []Message, limit int) string {
	var builder strings.Builder
	start := max(len(messages)-12, 0)
	for _, message := range messages[start:] {
		content := strings.Join(strings.Fields(message.Content), " ")
		content = truncateUTF8(content, 320)
		fmt.Fprintf(&builder, "- %s: %s\n", message.Role, content)
		for _, link := range URLPattern.FindAllString(message.Content, 4) {
			fmt.Fprintf(&builder, "  source: %s\n", link)
		}
	}
	return truncateUTF8(strings.TrimSpace(builder.String()), limit)
}

func renderPrompt(messages []Message) string {
	if len(messages) == 1 && messages[0].Role == "user" {
		return messages[0].Content
	}
	var builder strings.Builder
	builder.WriteString("[Session context]\n")
	for _, message := range messages {
		label := "User"
		if message.Role == "assistant" {
			label = "Assistant"
		}
		fmt.Fprintf(&builder, "\n%s:\n%s\n", label, message.Content)
	}
	return strings.TrimSpace(builder.String())
}

func renderEvents(events []Event, limit int) string {
	if len(events) == 0 || limit <= 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("<structured_session_events>\n")
	for _, event := range events {
		if event.Type != "tool_call" && event.Type != "tool_succeeded" && event.Type != "tool_failed" && event.Type != "metrics_recorded" && event.Type != "run_stopped" && event.Type != "run_failed" {
			continue
		}
		line := fmt.Sprintf("%s run=%s payload=%s\n", event.Type, event.RunID, strings.TrimSpace(string(event.Payload)))
		if builder.Len()+len(line)+len("</structured_session_events>") > limit {
			break
		}
		builder.WriteString(line)
	}
	if builder.Len() == len("<structured_session_events>\n") {
		return ""
	}
	builder.WriteString("</structured_session_events>")
	return builder.String()
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value
}

func newSessionID() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return "session_" + hex.EncodeToString(random), nil
}
