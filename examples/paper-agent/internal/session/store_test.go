package session_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/session"
)

type fakeSummarizer struct {
	mu     sync.Mutex
	calls  int
	result string
	err    error
}

func (f *fakeSummarizer) Summarize(_ context.Context, _ session.SummaryRequest) (session.SummaryResult, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.err != nil {
		return session.SummaryResult{}, f.err
	}
	return session.SummaryResult{Text: f.result, InputTokens: 100, OutputTokens: 20}, nil
}

func TestSessionCompactionKeepsFullHistoryAndRecentTurns(t *testing.T) {
	store, err := session.NewStore(t.TempDir()+"/sessions.db", session.Config{
		TriggerBytes: 180, TriggerTokens: 10_000, MinRecentTurns: 2,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	sessionID, err := store.Ensure(ctx, "")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	for index := 1; index <= 5; index++ {
		if err := store.AppendTurn(ctx, sessionID,
			fmt.Sprintf("user-%d %s", index, strings.Repeat("u", 25)),
			fmt.Sprintf("assistant-%d https://example.com/%d %s", index, index, strings.Repeat("a", 25)), 100,
		); err != nil {
			t.Fatalf("append turn %d: %v", index, err)
		}
	}
	view, err := store.BuildPrompt(ctx, sessionID, "current request")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if !view.Info.Compacted || view.Info.TriggeredBy != "bytes" || view.Info.DroppedMessages == 0 {
		t.Fatalf("compaction info = %#v", view.Info)
	}
	if !strings.Contains(view.Prompt, "session_digest") || !strings.Contains(view.Prompt, "current request") {
		t.Fatalf("prompt = %s", view.Prompt)
	}
	messages, err := store.Messages(ctx, sessionID)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(messages) != 10 {
		t.Fatalf("persistent history length = %d, want 10", len(messages))
	}
}

func TestForceCompactionKeepsOnlyCurrentRequest(t *testing.T) {
	store, err := session.NewStore(t.TempDir()+"/sessions.db", session.Config{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	sessionID, _ := store.Ensure(ctx, "")
	if err := store.AppendTurn(ctx, sessionID, "old question", "old answer", 200_000); err != nil {
		t.Fatalf("append: %v", err)
	}
	view, err := store.ForcePrompt(ctx, sessionID, "current", 0)
	if err != nil {
		t.Fatalf("force prompt: %v", err)
	}
	if !view.Info.Compacted || view.Info.TriggeredBy != "context_limit" {
		t.Fatalf("info = %#v", view.Info)
	}
	if strings.Contains(view.Prompt, "Assistant:\nold answer") || !strings.Contains(view.Prompt, "current") {
		t.Fatalf("prompt = %s", view.Prompt)
	}
	status, err := store.Status(ctx, sessionID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.MessageCount != 2 || status.LastInputTokens != 200_000 {
		t.Fatalf("status = %#v", status)
	}
}

func TestSessionCompactionUsesPrewarmedLLMSummary(t *testing.T) {
	summarizer := &fakeSummarizer{result: "用户目标是比较 Agent Memory；已经确认 A-MEM 的动态链接机制，下一步核对实验局限。"}
	store, err := session.NewStore(t.TempDir()+"/sessions.db", session.Config{
		TriggerBytes: 300, TriggerTokens: 10_000, MinRecentTurns: 2,
		Summarizer: summarizer, SummaryTimeout: time.Second, SummaryPrewarmRatio: 0.5,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	sessionID, _ := store.Ensure(ctx, "")
	for index := 1; index <= 4; index++ {
		if err := store.AppendTurn(ctx, sessionID,
			fmt.Sprintf("question-%d %s", index, strings.Repeat("q", 35)),
			fmt.Sprintf("answer-%d %s", index, strings.Repeat("a", 35)), 100,
		); err != nil {
			t.Fatalf("append turn %d: %v", index, err)
		}
	}
	waitForSummaryState(t, store, sessionID, "ready", 4)

	view, err := store.BuildPrompt(ctx, sessionID, "继续分析工程落地")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if !view.Info.Compacted || !view.Info.LLMSummaryUsed {
		t.Fatalf("compaction info = %#v", view.Info)
	}
	if !strings.Contains(view.Prompt, "<session_digest>") ||
		!strings.Contains(view.Prompt, "<conversation_summary>") ||
		!strings.Contains(view.Prompt, "A-MEM") {
		t.Fatalf("prompt lacks layered summaries: %s", view.Prompt)
	}
	status, err := store.Status(ctx, sessionID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.SummaryState != "idle" || status.MessageCount != 8 ||
		status.SummaryCalls < 1 || status.SummaryInputTokens < 100 || status.SummaryOutputTokens < 20 {
		t.Fatalf("status after consuming summary = %#v", status)
	}
}

func TestSessionSummaryFailureFallsBackToDeterministicDigest(t *testing.T) {
	store, err := session.NewStore(t.TempDir()+"/sessions.db", session.Config{
		TriggerBytes: 180, TriggerTokens: 10_000, MinRecentTurns: 1,
		Summarizer:     &fakeSummarizer{err: errors.New("summary unavailable")},
		SummaryTimeout: time.Second, SummaryPrewarmRatio: 0.5,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	sessionID, _ := store.Ensure(ctx, "")
	for index := 0; index < 3; index++ {
		if err := store.AppendTurn(ctx, sessionID, strings.Repeat("u", 40), strings.Repeat("a", 40), 100); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	waitForSummaryState(t, store, sessionID, "idle", 0)
	view, err := store.BuildPrompt(ctx, sessionID, "current")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	if !view.Info.Compacted || view.Info.LLMSummaryUsed || !strings.Contains(view.Prompt, "<session_digest>") {
		t.Fatalf("fallback view = %#v\n%s", view.Info, view.Prompt)
	}
}

func TestSessionListSaveAndFork(t *testing.T) {
	store, err := session.NewStore(t.TempDir()+"/sessions.db", session.Config{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	sourceID, _ := store.Ensure(ctx, "")
	if err := store.AppendTurn(ctx, sourceID, "question", "answer", 42); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := store.UpdateTitle(ctx, sourceID, "Memory notes"); err != nil {
		t.Fatalf("save title: %v", err)
	}
	forkID, err := store.Fork(ctx, sourceID, "Memory experiment")
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	forkStatus, err := store.Status(ctx, forkID)
	if err != nil {
		t.Fatalf("fork status: %v", err)
	}
	if forkStatus.Title != "Memory experiment" || forkStatus.ForkedFromID != sourceID || forkStatus.MessageCount != 2 {
		t.Fatalf("fork status = %#v", forkStatus)
	}
	items, err := store.List(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 || items[0].SessionID != forkID {
		t.Fatalf("session list = %#v", items)
	}
}

func TestSessionStoreMigratesExistingDatabase(t *testing.T) {
	path := t.TempDir() + "/sessions.db"
	database, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = database.Exec(`
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    last_input_tokens INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
INSERT INTO sessions(id, title, last_input_tokens, created_at, updated_at)
VALUES('legacy', 'Legacy session', 123, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');`)
	if err != nil {
		database.Close()
		t.Fatalf("create legacy db: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	store, err := session.NewStore(path, session.Config{})
	if err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	defer store.Close()
	status, err := store.Status(context.Background(), "legacy")
	if err != nil {
		t.Fatalf("read migrated status: %v", err)
	}
	if status.Title != "Legacy session" || status.LastInputTokens != 123 || status.SummaryCalls != 0 {
		t.Fatalf("migrated status = %#v", status)
	}
}

func waitForSummaryState(t *testing.T, store *session.Store, sessionID, want string, covered int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, err := store.Status(context.Background(), sessionID)
		if err == nil && status.SummaryState == want && status.SummaryCovered >= covered {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	status, _ := store.Status(context.Background(), sessionID)
	t.Fatalf("summary state = %q coverage=%d, want %q coverage >= %d", status.SummaryState, status.SummaryCovered, want, covered)
}
