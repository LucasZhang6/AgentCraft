package subagent

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/sqliteutil"
)

type Status string

const (
	StatusPending     Status = "pending"
	StatusRunning     Status = "running"
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	StatusCanceled    Status = "canceled"
	StatusTimedOut    Status = "timed_out"
	StatusInterrupted Status = "interrupted"
)

type Record struct {
	ID              string     `json:"id"`
	ParentRunID     string     `json:"parent_run_id,omitempty"`
	ParentSessionID string     `json:"parent_session_id,omitempty"`
	Label           string     `json:"label,omitempty"`
	Task            string     `json:"task"`
	Status          Status     `json:"status"`
	Result          string     `json:"result,omitempty"`
	Error           string     `json:"error,omitempty"`
	TimeoutSeconds  int        `json:"timeout_seconds"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

func (record Record) Terminal() bool {
	switch record.Status {
	case StatusCompleted, StatusFailed, StatusCanceled, StatusTimedOut, StatusInterrupted:
		return true
	default:
		return false
	}
}

type Input struct {
	ParentRunID     string
	ParentSessionID string
	Label           string
	Task            string
	Timeout         time.Duration
}

type Runner func(context.Context, string) (string, error)

type activeRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type Manager struct {
	db     *sql.DB
	mu     sync.Mutex
	active map[string]activeRun
	closed bool
	wg     sync.WaitGroup
}

func NewManager(path string) (*Manager, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sqliteutil.Open(path, false)
	if err != nil {
		return nil, err
	}
	manager := &Manager{db: db, active: make(map[string]activeRun)}
	if err := manager.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return manager, nil
}

func (m *Manager) init() error {
	_, err := m.db.Exec(`
CREATE TABLE IF NOT EXISTS subagents (
  id TEXT PRIMARY KEY,
  parent_run_id TEXT NOT NULL DEFAULT '',
  parent_session_id TEXT NOT NULL DEFAULT '',
  label TEXT NOT NULL DEFAULT '',
  task TEXT NOT NULL,
  status TEXT NOT NULL,
  result TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  timeout_seconds INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  started_at INTEGER,
  finished_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_subagents_session_created ON subagents(parent_session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_subagents_run_created ON subagents(parent_run_id, created_at DESC);
`)
	if err != nil {
		return fmt.Errorf("initialize subagent store: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	_, err = m.db.Exec(`UPDATE subagents SET status = ?, error = ?, finished_at = ? WHERE status IN (?, ?)`,
		StatusInterrupted, "process restarted before the subagent reached a terminal state", now, StatusPending, StatusRunning)
	return err
}

func (m *Manager) Spawn(parent context.Context, input Input, runner Runner) (Record, error) {
	if runner == nil {
		return Record{}, errors.New("subagent runner is unavailable")
	}
	input.Task = strings.TrimSpace(input.Task)
	if input.Task == "" {
		return Record{}, errors.New("subagent task is required")
	}
	if input.Timeout <= 0 {
		input.Timeout = 10 * time.Minute
	}
	id, err := newID()
	if err != nil {
		return Record{}, err
	}
	now := time.Now().UTC()
	record := Record{
		ID: id, ParentRunID: strings.TrimSpace(input.ParentRunID), ParentSessionID: strings.TrimSpace(input.ParentSessionID),
		Label: strings.TrimSpace(input.Label), Task: input.Task, Status: StatusPending,
		TimeoutSeconds: int(input.Timeout.Seconds()), CreatedAt: now,
	}
	if _, err := m.db.Exec(`INSERT INTO subagents(id, parent_run_id, parent_session_id, label, task, status, timeout_seconds, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ParentRunID, record.ParentSessionID, record.Label, record.Task, record.Status, record.TimeoutSeconds, now.UnixMilli()); err != nil {
		return Record{}, err
	}

	if parent == nil {
		parent = context.Background()
	} else if err := parent.Err(); err != nil {
		return Record{}, fmt.Errorf("subagent parent context is already done: %w", err)
	} else {
		// A child must outlive the short-lived tool call that spawned it. Values are
		// retained, while timeout and explicit Cancel remain lifecycle authorities.
		parent = context.WithoutCancel(parent)
	}
	ctx, cancel := context.WithTimeout(parent, input.Timeout)
	done := make(chan struct{})
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		_, _ = m.db.Exec(`UPDATE subagents SET status = ?, error = ?, finished_at = ? WHERE id = ?`, StatusInterrupted, "subagent manager is closed", time.Now().UTC().UnixMilli(), id)
		return Record{}, errors.New("subagent manager is closed")
	}
	m.active[id] = activeRun{cancel: cancel, done: done}
	m.wg.Add(1)
	m.mu.Unlock()
	go m.run(ctx, id, input.Task, runner, done)
	return record, nil
}

func (m *Manager) run(ctx context.Context, id, task string, runner Runner, done chan struct{}) {
	defer m.wg.Done()
	defer close(done)
	started := time.Now().UTC()
	_, _ = m.db.Exec(`UPDATE subagents SET status = ?, started_at = ? WHERE id = ? AND status = ?`, StatusRunning, started.UnixMilli(), id, StatusPending)
	result, runErr := runner(ctx, task)
	finished := time.Now().UTC()
	status := StatusCompleted
	errorText := ""
	if runErr != nil {
		errorText = runErr.Error()
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			status = StatusTimedOut
		case errors.Is(ctx.Err(), context.Canceled):
			status = StatusCanceled
		default:
			status = StatusFailed
		}
	}
	_, _ = m.db.Exec(`UPDATE subagents SET status = ?, result = ?, error = ?, finished_at = ? WHERE id = ?`, status, result, errorText, finished.UnixMilli(), id)
	m.mu.Lock()
	delete(m.active, id)
	m.mu.Unlock()
}

func (m *Manager) Get(ctx context.Context, id string) (Record, error) {
	row := m.db.QueryRowContext(ctx, `SELECT id, parent_run_id, parent_session_id, label, task, status, result, error, timeout_seconds, created_at, started_at, finished_at FROM subagents WHERE id = ?`, strings.TrimSpace(id))
	return scanRecord(row)
}

func (m *Manager) List(ctx context.Context, sessionID, runID string, limit int) ([]Record, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := `SELECT id, parent_run_id, parent_session_id, label, task, status, result, error, timeout_seconds, created_at, started_at, finished_at FROM subagents`
	var args []any
	switch {
	case strings.TrimSpace(sessionID) != "":
		query += ` WHERE parent_session_id = ?`
		args = append(args, strings.TrimSpace(sessionID))
	case strings.TrimSpace(runID) != "":
		query += ` WHERE parent_run_id = ?`
		args = append(args, strings.TrimSpace(runID))
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []Record
	for rows.Next() {
		record, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (m *Manager) Wait(ctx context.Context, id string) (Record, error) {
	id = strings.TrimSpace(id)
	for {
		record, err := m.Get(ctx, id)
		if err != nil || record.Terminal() {
			return record, err
		}
		m.mu.Lock()
		active, ok := m.active[id]
		m.mu.Unlock()
		if ok {
			select {
			case <-ctx.Done():
				return Record{}, ctx.Err()
			case <-active.done:
			}
			continue
		}
		select {
		case <-ctx.Done():
			return Record{}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (m *Manager) Cancel(ctx context.Context, id string) (Record, error) {
	id = strings.TrimSpace(id)
	record, err := m.Get(ctx, id)
	if err != nil || record.Terminal() {
		return record, err
	}
	m.mu.Lock()
	active, ok := m.active[id]
	m.mu.Unlock()
	if !ok {
		return Record{}, errors.New("subagent is not active in this process")
	}
	active.cancel()
	return m.Wait(ctx, id)
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	for _, active := range m.active {
		active.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
	return m.db.Close()
}

type scanner interface {
	Scan(...any) error
}

func scanRecord(row scanner) (Record, error) {
	var record Record
	var created int64
	var started, finished sql.NullInt64
	if err := row.Scan(&record.ID, &record.ParentRunID, &record.ParentSessionID, &record.Label, &record.Task, &record.Status, &record.Result, &record.Error, &record.TimeoutSeconds, &created, &started, &finished); err != nil {
		return Record{}, err
	}
	record.CreatedAt = time.UnixMilli(created).UTC()
	if started.Valid {
		value := time.UnixMilli(started.Int64).UTC()
		record.StartedAt = &value
	}
	if finished.Valid {
		value := time.UnixMilli(finished.Int64).UTC()
		record.FinishedAt = &value
	}
	return record, nil
}

func newID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "subagent_" + hex.EncodeToString(buffer), nil
}
