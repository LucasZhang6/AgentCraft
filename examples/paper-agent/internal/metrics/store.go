package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

type Summary struct {
	Runs            int     `json:"runs"`
	SuccessfulRuns  int     `json:"successfulRuns"`
	TaskSuccessRate float64 `json:"taskSuccessRate"`
	AverageDuration float64 `json:"averageDurationMs"`
	TotalTokens     int     `json:"totalTokens"`
	CacheReadTokens int     `json:"cacheReadInputTokens"`
	TotalToolCalls  int     `json:"totalToolCalls"`
}

type Recorder struct {
	store *Store
	runID string
}

func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("metrics database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create metrics directory: %w", err)
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_busy_timeout=5000&_journal_mode=WAL"}).String()
	database, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open metrics database: %w", err)
	}
	database.SetMaxOpenConns(1)
	store := &Store{db: database}
	if err := store.init(); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) init() error {
	const schema = `
CREATE TABLE IF NOT EXISTS run_metrics (
    run_id TEXT PRIMARY KEY,
    started_at TEXT NOT NULL,
    completed_at TEXT NOT NULL,
    duration_ms INTEGER NOT NULL,
    llm_calls INTEGER NOT NULL,
    input_tokens INTEGER NOT NULL,
	    output_tokens INTEGER NOT NULL,
	    total_tokens INTEGER NOT NULL,
	    cache_read_input_tokens INTEGER NOT NULL DEFAULT 0,
	    cache_creation_input_tokens INTEGER NOT NULL DEFAULT 0,
    tool_calls INTEGER NOT NULL,
    tool_failures INTEGER NOT NULL,
    tool_duration_ms INTEGER NOT NULL,
    human_approval_requests INTEGER NOT NULL,
    context_compactions INTEGER NOT NULL,
    goal_turns INTEGER NOT NULL,
    success INTEGER NOT NULL
);`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("initialize metrics database: %w", err)
	}
	for _, statement := range []string{
		`ALTER TABLE run_metrics ADD COLUMN cache_read_input_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE run_metrics ADD COLUMN cache_creation_input_tokens INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := s.db.Exec(statement); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("migrate metrics database: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Bind(runID string) Recorder {
	return Recorder{store: s, runID: strings.TrimSpace(runID)}
}

func (r Recorder) Record(ctx context.Context, metrics domain.RunMetrics) error {
	if r.store == nil || r.store.db == nil {
		return errors.New("metrics store is not initialized")
	}
	if r.runID == "" {
		return errors.New("metrics run id is required")
	}
	success := 0
	if metrics.Success {
		success = 1
	}
	_, err := r.store.db.ExecContext(ctx, `
INSERT INTO run_metrics(
    run_id, started_at, completed_at, duration_ms, llm_calls,
	    input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, tool_calls, tool_failures,
    tool_duration_ms, human_approval_requests, context_compactions, goal_turns, success
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
    completed_at = excluded.completed_at,
    duration_ms = excluded.duration_ms,
    llm_calls = excluded.llm_calls,
    input_tokens = excluded.input_tokens,
    output_tokens = excluded.output_tokens,
	    total_tokens = excluded.total_tokens,
	    cache_read_input_tokens = excluded.cache_read_input_tokens,
	    cache_creation_input_tokens = excluded.cache_creation_input_tokens,
    tool_calls = excluded.tool_calls,
    tool_failures = excluded.tool_failures,
    tool_duration_ms = excluded.tool_duration_ms,
    human_approval_requests = excluded.human_approval_requests,
    context_compactions = excluded.context_compactions,
    goal_turns = excluded.goal_turns,
    success = excluded.success`,
		r.runID, metrics.StartedAt.UTC().Format(timeFormat), metrics.CompletedAt.UTC().Format(timeFormat),
		metrics.DurationMS, metrics.LLMCalls, metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens,
		metrics.CacheReadInputTokens, metrics.CacheCreationInputTokens,
		metrics.ToolCalls, metrics.ToolFailures, metrics.ToolDurationMS, metrics.HumanApprovalRequests,
		metrics.ContextCompactions, metrics.GoalTurns, success,
	)
	if err != nil {
		return fmt.Errorf("record run metrics: %w", err)
	}
	return nil
}

func (s *Store) Summary(ctx context.Context) (Summary, error) {
	var summary Summary
	var duration sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*), COALESCE(SUM(success), 0), AVG(duration_ms),
	       COALESCE(SUM(total_tokens), 0), COALESCE(SUM(cache_read_input_tokens), 0), COALESCE(SUM(tool_calls), 0)
FROM run_metrics`).Scan(
		&summary.Runs, &summary.SuccessfulRuns, &duration, &summary.TotalTokens, &summary.CacheReadTokens, &summary.TotalToolCalls,
	)
	if err != nil {
		return Summary{}, fmt.Errorf("summarize run metrics: %w", err)
	}
	if summary.Runs > 0 {
		summary.TaskSuccessRate = float64(summary.SuccessfulRuns) / float64(summary.Runs)
	}
	if duration.Valid {
		summary.AverageDuration = duration.Float64
	}
	return summary, nil
}

const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"
