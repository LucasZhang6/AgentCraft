package feishu

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/sqliteutil"
)

type Store struct {
	db *sql.DB
}

type Mapping struct {
	Key        string
	TenantKey  string
	ChatID     string
	ChatType   string
	UserID     string
	SessionID  string
	LastTaskID string
	UpdatedAt  time.Time
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		path = defaultDBPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sqliteutil.Open(path, true)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS feishu_session_map (
	key TEXT PRIMARY KEY,
	tenant_key TEXT,
	chat_id TEXT NOT NULL,
	chat_type TEXT,
	user_id TEXT,
	session_id TEXT,
	last_task_id TEXT,
	updated_at TIMESTAMP NOT NULL
);
CREATE TABLE IF NOT EXISTS feishu_processed_events (
	event_id TEXT PRIMARY KEY,
	created_at TIMESTAMP NOT NULL
);`)
	return err
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) MarkEvent(eventID string) (bool, error) {
	if eventID == "" {
		return true, nil
	}
	_, err := s.db.Exec(`INSERT INTO feishu_processed_events (event_id, created_at) VALUES (?, ?)`, eventID, time.Now())
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return false, nil
	}
	return false, err
}

func (s *Store) GetMapping(key string) (*Mapping, error) {
	var m Mapping
	err := s.db.QueryRow(`
SELECT key, tenant_key, chat_id, chat_type, user_id, session_id, last_task_id, updated_at
FROM feishu_session_map WHERE key = ?`, key).Scan(
		&m.Key, &m.TenantKey, &m.ChatID, &m.ChatType, &m.UserID, &m.SessionID, &m.LastTaskID, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) UpsertMapping(m Mapping) error {
	m.UpdatedAt = time.Now()
	_, err := s.db.Exec(`
INSERT INTO feishu_session_map (key, tenant_key, chat_id, chat_type, user_id, session_id, last_task_id, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
	tenant_key=excluded.tenant_key,
	chat_id=excluded.chat_id,
	chat_type=excluded.chat_type,
	user_id=excluded.user_id,
	session_id=excluded.session_id,
	last_task_id=excluded.last_task_id,
	updated_at=excluded.updated_at`,
		m.Key, m.TenantKey, m.ChatID, m.ChatType, m.UserID, m.SessionID, m.LastTaskID, m.UpdatedAt)
	return err
}

func (s *Store) ClearSession(key string) error {
	_, err := s.db.Exec(`UPDATE feishu_session_map SET session_id = '', last_task_id = '', updated_at = ? WHERE key = ?`, time.Now(), key)
	return err
}
