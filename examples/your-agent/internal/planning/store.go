package planning

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/sqliteutil"
)

type Plan struct {
	ID        string            `json:"id"`
	SessionID string            `json:"sessionId"`
	RunID     string            `json:"runId"`
	Objective string            `json:"objective"`
	Status    string            `json:"status"`
	Steps     []domain.PlanStep `json:"steps"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("plan database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	database, err := sqliteutil.Open(path, false)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	store := &Store{db: database, now: time.Now}
	if _, err := database.Exec(`
CREATE TABLE IF NOT EXISTS plans (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, run_id TEXT NOT NULL UNIQUE,
  objective TEXT NOT NULL, status TEXT NOT NULL, steps_json TEXT NOT NULL,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_plans_session_updated ON plans(session_id, updated_at DESC);`); err != nil {
		_ = database.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) Save(ctx context.Context, sessionID, runID, objective string, steps []domain.PlanStep) (Plan, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(runID) == "" || strings.TrimSpace(objective) == "" {
		return Plan{}, errors.New("session id, run id, and objective are required")
	}
	id, err := planID()
	if err != nil {
		return Plan{}, err
	}
	now := store.now().UTC()
	encoded, err := json.Marshal(steps)
	if err != nil {
		return Plan{}, err
	}
	_, err = store.db.ExecContext(ctx, `INSERT INTO plans(id,session_id,run_id,objective,status,steps_json,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(run_id) DO UPDATE SET objective=excluded.objective,status=excluded.status,
steps_json=excluded.steps_json,updated_at=excluded.updated_at`, id, sessionID, runID, objective,
		planStatus(steps), string(encoded), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Plan{}, err
	}
	return store.ByRun(ctx, runID)
}

func (store *Store) Update(ctx context.Context, id string, steps []domain.PlanStep) (Plan, error) {
	encoded, err := json.Marshal(steps)
	if err != nil {
		return Plan{}, err
	}
	result, err := store.db.ExecContext(ctx, `UPDATE plans SET status=?,steps_json=?,updated_at=? WHERE id=?`,
		planStatus(steps), string(encoded), store.now().UTC().Format(time.RFC3339Nano), strings.TrimSpace(id))
	if err != nil {
		return Plan{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return Plan{}, sql.ErrNoRows
	}
	return store.Get(ctx, id)
}

func (store *Store) Get(ctx context.Context, id string) (Plan, error) {
	return store.scan(store.db.QueryRowContext(ctx, `SELECT id,session_id,run_id,objective,status,steps_json,created_at,updated_at FROM plans WHERE id=?`, strings.TrimSpace(id)))
}

func (store *Store) ByRun(ctx context.Context, runID string) (Plan, error) {
	return store.scan(store.db.QueryRowContext(ctx, `SELECT id,session_id,run_id,objective,status,steps_json,created_at,updated_at FROM plans WHERE run_id=?`, strings.TrimSpace(runID)))
}

func (store *Store) Latest(ctx context.Context, sessionID string) (Plan, error) {
	return store.scan(store.db.QueryRowContext(ctx, `SELECT id,session_id,run_id,objective,status,steps_json,created_at,updated_at FROM plans WHERE session_id=? ORDER BY updated_at DESC LIMIT 1`, strings.TrimSpace(sessionID)))
}

func (store *Store) List(ctx context.Context, sessionID string, limit int) ([]Plan, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id,session_id,run_id,objective,status,steps_json,created_at,updated_at FROM plans WHERE session_id=? ORDER BY updated_at DESC LIMIT ?`, strings.TrimSpace(sessionID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var plans []Plan
	for rows.Next() {
		item, err := store.scan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, item)
	}
	return plans, rows.Err()
}

type scanner interface{ Scan(...any) error }

func (store *Store) scan(row scanner) (Plan, error) {
	var item Plan
	var steps, created, updated string
	if err := row.Scan(&item.ID, &item.SessionID, &item.RunID, &item.Objective, &item.Status, &steps, &created, &updated); err != nil {
		return Plan{}, err
	}
	if err := json.Unmarshal([]byte(steps), &item.Steps); err != nil {
		return Plan{}, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return item, nil
}

func planStatus(steps []domain.PlanStep) string {
	if len(steps) == 0 {
		return "empty"
	}
	allCompleted := true
	for _, step := range steps {
		if step.Status == domain.PlanFailed {
			return domain.PlanFailed
		}
		if step.Status == domain.PlanWaiting {
			return domain.PlanWaiting
		}
		if step.Status != domain.PlanCompleted && step.Status != domain.PlanSkipped {
			allCompleted = false
		}
	}
	if allCompleted {
		return domain.PlanCompleted
	}
	return domain.PlanRunning
}

func planID() (string, error) {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return "plan_" + hex.EncodeToString(data), nil
}

type Recorder struct {
	store     *Store
	sessionID string
	runID     string
	planID    string
}

func (store *Store) Bind(sessionID, runID string) *Recorder {
	return &Recorder{store: store, sessionID: sessionID, runID: runID}
}

func NewRecorder(store *Store, sessionID, runID string) *Recorder {
	return store.Bind(sessionID, runID)
}

func (recorder *Recorder) Save(ctx context.Context, objective string, steps []domain.PlanStep) error {
	item, err := recorder.store.Save(ctx, recorder.sessionID, recorder.runID, objective, steps)
	if err == nil {
		recorder.planID = item.ID
	}
	return err
}

func (recorder *Recorder) Update(ctx context.Context, steps []domain.PlanStep) error {
	if recorder == nil || recorder.planID == "" {
		return nil
	}
	_, err := recorder.store.Update(ctx, recorder.planID, steps)
	return err
}
