package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/sqliteutil"
)

type taskRecord struct {
	ID               string
	SessionID        string
	UserMessage      string
	GoalID           string
	Status           string
	Result           string
	Error            string
	Messages         []string
	PendingApproval  *PendingApproval
	ApproveRemaining bool
	Complete         bool
	Success          bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type taskStore struct {
	db *sql.DB
}

func newTaskStore(path string) (*taskStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sqliteutil.Open(path, false)
	if err != nil {
		return nil, err
	}
	store := &taskStore{db: db}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *taskStore) init() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS server_tasks (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  user_message TEXT NOT NULL DEFAULT '',
  goal_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  result TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  messages_json TEXT NOT NULL DEFAULT '[]',
  pending_approval_json TEXT NOT NULL DEFAULT '',
  approve_remaining INTEGER NOT NULL DEFAULT 0,
  complete INTEGER NOT NULL DEFAULT 0,
  success INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_server_tasks_updated ON server_tasks(updated_at DESC);
`)
	if err != nil {
		return fmt.Errorf("initialize task store: %w", err)
	}
	return nil
}

func (s *taskStore) interruptActive(ctx context.Context) error {
	now := time.Now().UTC().UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE server_tasks SET status = 'interrupted', error = ?, pending_approval_json = '', complete = 1, success = 0, updated_at = ? WHERE complete = 0`,
		"server restarted before the task reached a terminal state", now)
	return err
}

func (s *taskStore) save(ctx context.Context, record taskRecord) error {
	messages, err := json.Marshal(record.Messages)
	if err != nil {
		return err
	}
	pending := ""
	if record.PendingApproval != nil {
		data, err := json.Marshal(record.PendingApproval)
		if err != nil {
			return err
		}
		pending = string(data)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO server_tasks(id, session_id, user_message, goal_id, status, result, error, messages_json, pending_approval_json, approve_remaining, complete, success, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  session_id = excluded.session_id, user_message = excluded.user_message, goal_id = excluded.goal_id,
  status = excluded.status, result = excluded.result, error = excluded.error, messages_json = excluded.messages_json,
  pending_approval_json = excluded.pending_approval_json, approve_remaining = excluded.approve_remaining,
  complete = excluded.complete, success = excluded.success, updated_at = excluded.updated_at`,
		record.ID, record.SessionID, record.UserMessage, record.GoalID, record.Status, record.Result, record.Error,
		string(messages), pending, record.ApproveRemaining, record.Complete, record.Success,
		record.CreatedAt.UnixMilli(), record.UpdatedAt.UnixMilli())
	return err
}

func (s *taskStore) list(ctx context.Context, limit int) ([]taskRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, session_id, user_message, goal_id, status, result, error, messages_json, pending_approval_json, approve_remaining, complete, success, created_at, updated_at FROM server_tasks ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []taskRecord
	for rows.Next() {
		var record taskRecord
		var messages, pending string
		var approveRemaining, complete, success bool
		var created, updated int64
		if err := rows.Scan(&record.ID, &record.SessionID, &record.UserMessage, &record.GoalID, &record.Status, &record.Result, &record.Error,
			&messages, &pending, &approveRemaining, &complete, &success, &created, &updated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(messages), &record.Messages); err != nil {
			return nil, err
		}
		if pending != "" {
			var approval PendingApproval
			if err := json.Unmarshal([]byte(pending), &approval); err != nil {
				return nil, err
			}
			record.PendingApproval = &approval
		}
		record.ApproveRemaining, record.Complete, record.Success = approveRemaining, complete, success
		record.CreatedAt, record.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *taskStore) delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM server_tasks WHERE id = ? AND complete = 1`, id)
	if err != nil {
		return err
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

func (s *taskStore) close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
