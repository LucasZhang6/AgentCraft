package memory

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/sqliteutil"
)

const (
	defaultRetrieveLimit = 8
	defaultRetrieveBytes = 4000
)

var sensitiveMemory = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|password|authorization\s*:\s*bearer|sk-[a-z0-9_-]{12,})`)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("memory database path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create memory directory: %w", err)
	}
	database, err := sqliteutil.Open(path, true)
	if err != nil {
		return nil, fmt.Errorf("open memory database: %w", err)
	}
	database.SetMaxOpenConns(1)
	store := &Store{db: database, now: time.Now}
	if err := store.init(); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) init() error {
	const schema = `
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    source TEXT NOT NULL,
    confidence REAL NOT NULL,
    scope TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_used_at TEXT NOT NULL DEFAULT '',
    UNIQUE(scope, key)
);
CREATE INDEX IF NOT EXISTS idx_memories_status_scope ON memories(status, scope);
CREATE INDEX IF NOT EXISTS idx_memories_updated_at ON memories(updated_at DESC);`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("initialize memory database: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Remember(ctx context.Context, input domain.MemoryInput) (domain.Memory, error) {
	if err := ctx.Err(); err != nil {
		return domain.Memory{}, err
	}
	input.Key = strings.TrimSpace(input.Key)
	input.Value = strings.TrimSpace(input.Value)
	input.Source = strings.TrimSpace(input.Source)
	input.Scope = normalizeScope(input.Scope)
	if input.Key == "" || input.Value == "" {
		return domain.Memory{}, errors.New("memory key and value are required")
	}
	if input.Source == "" {
		return domain.Memory{}, errors.New("memory source is required")
	}
	if sensitiveMemory.MatchString(input.Value) {
		return domain.Memory{}, errors.New("memory contains sensitive content")
	}
	if input.Confidence <= 0 {
		input.Confidence = 1
	}
	if input.Confidence > 1 {
		input.Confidence = 1
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	id, err := newMemoryID()
	if err != nil {
		return domain.Memory{}, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO memories(id, key, value, source, confidence, scope, status, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(scope, key) DO UPDATE SET
    value = excluded.value,
    source = excluded.source,
    confidence = excluded.confidence,
    status = excluded.status,
    updated_at = excluded.updated_at`,
		id, input.Key, input.Value, input.Source, input.Confidence, input.Scope,
		domain.MemoryActive, now, now,
	)
	if err != nil {
		return domain.Memory{}, fmt.Errorf("save memory: %w", err)
	}
	return s.getByScopeKey(ctx, input.Scope, input.Key)
}

func (s *Store) Retrieve(ctx context.Context, query domain.MemoryQuery) ([]domain.Memory, error) {
	if query.Limit <= 0 {
		query.Limit = defaultRetrieveLimit
	}
	if query.LimitBytes <= 0 {
		query.LimitBytes = defaultRetrieveBytes
	}
	items, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	allowedScopes := make(map[string]struct{}, len(query.Scopes))
	for _, scope := range query.Scopes {
		allowedScopes[normalizeScope(scope)] = struct{}{}
	}
	terms := queryTerms(query.Text)
	type candidate struct {
		item  domain.Memory
		score float64
	}
	candidates := make([]candidate, 0, len(items))
	for _, item := range items {
		if item.Status != domain.MemoryActive {
			continue
		}
		if len(allowedScopes) > 0 {
			if _, exists := allowedScopes[item.Scope]; !exists {
				continue
			}
		}
		candidates = append(candidates, candidate{item: item, score: memoryScore(item, terms)})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].item.UpdatedAt > candidates[j].item.UpdatedAt
		}
		return candidates[i].score > candidates[j].score
	})

	result := make([]domain.Memory, 0, query.Limit)
	usedBytes := 0
	for _, candidate := range candidates {
		if len(result) >= query.Limit {
			break
		}
		cost := len(candidate.item.Key) + len(candidate.item.Value) + len(candidate.item.Source) + 64
		if len(result) > 0 && usedBytes+cost > query.LimitBytes {
			break
		}
		result = append(result, candidate.item)
		usedBytes += cost
	}
	if err := s.touch(ctx, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) List(ctx context.Context) ([]domain.Memory, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, key, value, source, confidence, scope, status, created_at, updated_at, last_used_at
FROM memories
ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()
	var result []domain.Memory
	for rows.Next() {
		item, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories: %w", err)
	}
	return result, nil
}

func (s *Store) Forget(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE memories SET status = ?, updated_at = ? WHERE id = ?`,
		domain.MemoryArchived, s.now().UTC().Format(time.RFC3339Nano), strings.TrimSpace(id),
	)
	if err != nil {
		return fmt.Errorf("archive memory: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("memory not found: %s", id)
	}
	return nil
}

func (s *Store) getByScopeKey(ctx context.Context, scope, key string) (domain.Memory, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, key, value, source, confidence, scope, status, created_at, updated_at, last_used_at
FROM memories WHERE scope = ? AND key = ?`, scope, key)
	return scanMemory(row)
}

type rowScanner interface {
	Scan(...any) error
}

func scanMemory(row rowScanner) (domain.Memory, error) {
	var item domain.Memory
	if err := row.Scan(
		&item.ID, &item.Key, &item.Value, &item.Source, &item.Confidence,
		&item.Scope, &item.Status, &item.CreatedAt, &item.UpdatedAt, &item.LastUsedAt,
	); err != nil {
		return domain.Memory{}, fmt.Errorf("scan memory: %w", err)
	}
	return item, nil
}

func (s *Store) touch(ctx context.Context, memories []domain.Memory) error {
	if len(memories) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin memory touch: %w", err)
	}
	defer tx.Rollback()
	now := s.now().UTC().Format(time.RFC3339Nano)
	for _, item := range memories {
		if _, err := tx.ExecContext(ctx, `UPDATE memories SET last_used_at = ? WHERE id = ?`, now, item.ID); err != nil {
			return fmt.Errorf("touch memory %s: %w", item.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit memory touch: %w", err)
	}
	return nil
}

func normalizeScope(scope string) string {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		return "user"
	}
	return scope
}

func queryTerms(text string) []string {
	parts := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",，。:：/\\|;；()（）[]【】", r)
	})
	terms := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if len([]rune(part)) >= 2 {
			terms = append(terms, part)
		}
	}
	return terms
}

func memoryScore(item domain.Memory, terms []string) float64 {
	score := item.Confidence
	key := strings.ToLower(item.Key)
	value := strings.ToLower(item.Value)
	source := strings.ToLower(item.Source)
	for _, term := range terms {
		if strings.Contains(key, term) {
			score += 4
		}
		if strings.Contains(value, term) {
			score += 2
		}
		if strings.Contains(source, term) {
			score++
		}
	}
	return score
}

func newMemoryID() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate memory id: %w", err)
	}
	return "mem_" + hex.EncodeToString(random), nil
}
