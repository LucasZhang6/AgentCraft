package goal

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
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type State string

const (
	Pursuing      State = "pursuing"
	Paused        State = "paused"
	Achieved      State = "achieved"
	Unmet         State = "unmet"
	BudgetLimited State = "budget_limited"
)

type HistoryEntry struct {
	At        time.Time `json:"at"`
	From      State     `json:"from,omitempty"`
	To        State     `json:"to"`
	Iteration int       `json:"iteration"`
	Summary   string    `json:"summary,omitempty"`
}

type Goal struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"sessionId"`
	Objective  string         `json:"objective"`
	State      State          `json:"state"`
	AutoResume bool           `json:"autoResume"`
	TokensUsed int            `json:"tokensUsed"`
	Iterations int            `json:"iterations"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
	History    []HistoryEntry `json:"history"`
}

func (goal Goal) Active() bool { return goal.State == Pursuing }
func (goal Goal) Terminal() bool {
	return goal.State == Achieved || goal.State == Unmet || goal.State == BudgetLimited
}

func (goal Goal) ContinuationPrompt(direction string) string {
	direction = strings.TrimSpace(direction)
	if direction == "" {
		direction = "Continue from the persisted session evidence and verify the next incomplete step."
	}
	return fmt.Sprintf("[Persistent Goal]\nObjective: %s\nState: %s\nCompleted continuation turns: %d\nUser direction: %s\nContinue the same goal. Do not restart completed work.", goal.Objective, goal.State, goal.Iterations, direction)
}

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("goal database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_busy_timeout=5000&_journal_mode=WAL&_foreign_keys=on"}).String()
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	store := &Store{db: database, now: time.Now}
	if err := store.init(); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) init() error {
	_, err := store.db.Exec(`
CREATE TABLE IF NOT EXISTS goals (
  session_id TEXT PRIMARY KEY, id TEXT NOT NULL, objective TEXT NOT NULL, state TEXT NOT NULL,
  auto_resume INTEGER NOT NULL DEFAULT 0, tokens_used INTEGER NOT NULL DEFAULT 0,
  iterations INTEGER NOT NULL DEFAULT 0, history_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_goals_state_updated ON goals(state, updated_at DESC);`)
	return err
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) Set(ctx context.Context, sessionID, objective string) (Goal, error) {
	sessionID, objective = strings.TrimSpace(sessionID), strings.TrimSpace(objective)
	if sessionID == "" || objective == "" {
		return Goal{}, errors.New("session id and objective are required")
	}
	current, err := store.Get(ctx, sessionID)
	if err == nil && !current.Terminal() {
		return Goal{}, fmt.Errorf("session already has an active goal in state %s; clear it first", current.State)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Goal{}, err
	}
	id, err := randomID()
	if err != nil {
		return Goal{}, err
	}
	now := store.now().UTC()
	history := []HistoryEntry{{At: now, To: Pursuing}}
	encoded, _ := json.Marshal(history)
	_, err = store.db.ExecContext(ctx, `INSERT INTO goals(session_id,id,objective,state,history_json,created_at,updated_at)
VALUES(?,?,?,?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET id=excluded.id,objective=excluded.objective,state=excluded.state,
auto_resume=0,tokens_used=0,iterations=0,history_json=excluded.history_json,created_at=excluded.created_at,updated_at=excluded.updated_at`,
		sessionID, id, objective, Pursuing, string(encoded), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Goal{}, err
	}
	return store.Get(ctx, sessionID)
}

func (store *Store) Get(ctx context.Context, sessionID string) (Goal, error) {
	var result Goal
	var state, history, created, updated string
	var auto int
	err := store.db.QueryRowContext(ctx, `SELECT session_id,id,objective,state,auto_resume,tokens_used,iterations,history_json,created_at,updated_at FROM goals WHERE session_id=?`, strings.TrimSpace(sessionID)).Scan(
		&result.SessionID, &result.ID, &result.Objective, &state, &auto, &result.TokensUsed, &result.Iterations, &history, &created, &updated)
	if err != nil {
		return Goal{}, err
	}
	result.State, result.AutoResume = State(state), auto != 0
	_ = json.Unmarshal([]byte(history), &result.History)
	result.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	result.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return result, nil
}

func (store *Store) Pause(ctx context.Context, sessionID string) (Goal, error) {
	return store.transition(ctx, sessionID, Paused, false, "paused by user")
}
func (store *Store) Resume(ctx context.Context, sessionID string, auto bool) (Goal, error) {
	return store.transition(ctx, sessionID, Pursuing, auto, "resumed by user")
}
func (store *Store) Achieve(ctx context.Context, sessionID string, tokens, iterations int, summary string) (Goal, error) {
	return store.finish(ctx, sessionID, Achieved, tokens, iterations, summary)
}
func (store *Store) MarkUnmet(ctx context.Context, sessionID string, tokens, iterations int, summary string) (Goal, error) {
	return store.finish(ctx, sessionID, Unmet, tokens, iterations, summary)
}

func (store *Store) transition(ctx context.Context, sessionID string, to State, auto bool, summary string) (Goal, error) {
	current, err := store.Get(ctx, sessionID)
	if err != nil {
		return Goal{}, err
	}
	if current.Terminal() {
		return Goal{}, fmt.Errorf("goal is already terminal: %s", current.State)
	}
	if to == Paused && current.State != Pursuing {
		return Goal{}, fmt.Errorf("cannot pause goal in state %s", current.State)
	}
	if to == Pursuing && current.State != Paused {
		return Goal{}, fmt.Errorf("cannot resume goal in state %s", current.State)
	}
	return store.update(ctx, current, to, auto, current.TokensUsed, current.Iterations, summary)
}

func (store *Store) finish(ctx context.Context, sessionID string, state State, tokens, iterations int, summary string) (Goal, error) {
	current, err := store.Get(ctx, sessionID)
	if err != nil {
		return Goal{}, err
	}
	return store.update(ctx, current, state, false, max(tokens, current.TokensUsed), max(iterations, current.Iterations), summary)
}

func (store *Store) RecordProgress(ctx context.Context, sessionID string, tokens, iterations int, summary string) (Goal, error) {
	current, err := store.Get(ctx, sessionID)
	if err != nil {
		return Goal{}, err
	}
	return store.update(ctx, current, current.State, current.AutoResume, max(tokens, current.TokensUsed), max(iterations, current.Iterations), summary)
}

func (store *Store) update(ctx context.Context, current Goal, state State, auto bool, tokens, iterations int, summary string) (Goal, error) {
	now := store.now().UTC()
	current.History = append(current.History, HistoryEntry{At: now, From: current.State, To: state, Iteration: iterations, Summary: strings.TrimSpace(summary)})
	history, _ := json.Marshal(current.History)
	autoValue := 0
	if auto {
		autoValue = 1
	}
	_, err := store.db.ExecContext(ctx, `UPDATE goals SET state=?,auto_resume=?,tokens_used=?,iterations=?,history_json=?,updated_at=? WHERE session_id=?`, state, autoValue, tokens, iterations, string(history), now.Format(time.RFC3339Nano), current.SessionID)
	if err != nil {
		return Goal{}, err
	}
	return store.Get(ctx, current.SessionID)
}

func (store *Store) Clear(ctx context.Context, sessionID string) error {
	result, err := store.db.ExecContext(ctx, `DELETE FROM goals WHERE session_id=?`, strings.TrimSpace(sessionID))
	if err != nil {
		return err
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func randomID() (string, error) {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return "goal_" + hex.EncodeToString(data), nil
}
