package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/session"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/sqliteutil"
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
	if forkStatus.Title != "Memory experiment" || forkStatus.ForkedFromID != sourceID ||
		forkStatus.MessageCount != 2 || forkStatus.TurnCount != 1 || forkStatus.LastTurnStatus != string(session.TurnCompleted) {
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
	database, err := sqliteutil.Open(path, false)
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
CREATE TABLE session_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);
INSERT INTO sessions(id, title, last_input_tokens, created_at, updated_at)
VALUES('legacy', 'Legacy session', 123, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
INSERT INTO session_messages(session_id, role, content, created_at) VALUES
('legacy', 'user', 'old question', '2026-01-01T00:00:01Z'),
('legacy', 'assistant', 'old answer', '2026-01-01T00:00:02Z');`)
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
	if status.Title != "Legacy session" || status.LastInputTokens != 123 || status.SummaryCalls != 0 ||
		status.TurnCount != 1 || status.MessageCount != 2 || status.CanonicalEventCount != 3 || status.LastTurnStatus != string(session.TurnCompleted) {
		t.Fatalf("migrated status = %#v", status)
	}
	messages, err := store.Messages(context.Background(), "legacy")
	if err != nil || len(messages) != 2 || messages[0].TurnID == "" || messages[0].Sequence == 0 ||
		len(messages[0].Blocks) != 1 || messages[0].Blocks[0].Type != domain.BlockText {
		t.Fatalf("migrated messages = %#v, err = %v", messages, err)
	}
}

func TestStructuredTurnSurvivesRestartWithNativeSemantics(t *testing.T) {
	path := t.TempDir() + "/sessions.db"
	store, err := session.NewStore(path, session.Config{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	sessionID, err := store.Ensure(context.Background(), "structured")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	reasoningRaw := json.RawMessage(`{"type":"reasoning","id":"reason_1","encrypted_content":"ciphertext","summary":[{"type":"summary_text","text":"checked the plan"}]}`)
	turn, err := store.BeginTurn(context.Background(), sessionID, "run_1")
	if err != nil {
		t.Fatalf("begin turn: %v", err)
	}
	if err := turn.AddUser("compare agent memory", []string{"data:image/png;base64,aGVsbG8="}); err != nil {
		t.Fatalf("add user: %v", err)
	}
	if err := turn.AddRuntimeEvent("run_1", domain.Event{Type: "assistant_blocks", Payload: map[string]any{
		"phase": "decision",
		"blocks": []domain.ContentBlock{{
			Type: domain.BlockReasoning, ReasoningID: "reason_1", ReasoningSummary: "checked the plan",
			EncryptedContent: "ciphertext", Raw: reasoningRaw,
		}},
	}}); err != nil {
		t.Fatalf("add reasoning: %v", err)
	}
	if err := turn.AddRuntimeEvent("run_1", domain.Event{Type: "tool_call", Payload: map[string]any{
		"toolCallId": "call_1", "tool": "web_search", "args": map[string]any{"query": "A-MEM"},
	}}); err != nil {
		t.Fatalf("add tool call: %v", err)
	}
	if err := turn.AddRuntimeEvent("run_1", domain.Event{Type: "tool_succeeded", Payload: map[string]any{
		"observation": domain.Observation{ToolCallID: "call_1", Tool: "web_search", Result: map[string]any{"title": "A-MEM"}, OK: true},
	}}); err != nil {
		t.Fatalf("add tool result: %v", err)
	}
	if err := turn.AddAssistantText("A-MEM uses linked, evolving memories."); err != nil {
		t.Fatalf("add assistant: %v", err)
	}
	metrics := domain.RunMetrics{InputTokens: 101, OutputTokens: 20, TotalTokens: 121, ToolCalls: 1, Success: true}
	if err := turn.SetMetrics(metrics); err != nil {
		t.Fatalf("set metrics: %v", err)
	}
	if err := turn.Commit(context.Background(), session.TurnCompleted, ""); err != nil {
		t.Fatalf("commit: %v", err)
	}
	turnID := turn.ID()
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	store, err = session.NewStore(path, session.Config{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	messages, err := store.Messages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("messages: %v", err)
	}
	if len(messages) != 5 {
		t.Fatalf("messages = %d, want 5: %#v", len(messages), messages)
	}
	if messages[1].Role != "assistant_blocks" || messages[1].Blocks[0].Type != domain.BlockReasoning ||
		messages[1].Blocks[0].EncryptedContent != "ciphertext" || string(messages[1].Blocks[0].Raw) != string(reasoningRaw) {
		t.Fatalf("restored reasoning = %#v", messages[1])
	}
	if messages[2].Blocks[0].ToolCallID != "call_1" || messages[3].Blocks[0].ToolCallID != "call_1" || messages[3].Blocks[0].Type != domain.BlockToolResult {
		t.Fatalf("restored tool protocol = %#v / %#v", messages[2], messages[3])
	}
	status, err := store.Status(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.TurnCount != 1 || status.LastTurnStatus != string(session.TurnCompleted) || status.TotalInputTokens != 101 || status.TotalOutputTokens != 20 || status.TotalToolCalls != 1 {
		t.Fatalf("status = %#v", status)
	}
	restoredMetrics, err := store.MetricsForTurn(context.Background(), turnID)
	if err != nil || restoredMetrics.TotalTokens != 121 || restoredMetrics.ToolCalls != 1 {
		t.Fatalf("metrics = %#v, err = %v", restoredMetrics, err)
	}
	view, err := store.BuildPrompt(context.Background(), sessionID, "continue")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	native := view.NativeHistory()
	if len(native) != 5 || native[2].Blocks[0].Type != domain.BlockToolCall || native[3].Blocks[0].Type != domain.BlockToolResult {
		t.Fatalf("native history = %#v", native)
	}
}

func TestNativeParallelToolResultsRemainGroupedAcrossRestart(t *testing.T) {
	path := t.TempDir() + "/sessions.db"
	store, err := session.NewStore(path, session.Config{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	turn, err := store.BeginTurn(context.Background(), "parallel-native", "run_parallel")
	if err != nil {
		t.Fatalf("begin turn: %v", err)
	}
	if err := turn.AddUser("compare two sources", nil); err != nil {
		t.Fatalf("add user: %v", err)
	}
	calls := []domain.ContentBlock{
		{Type: domain.BlockToolCall, ToolCallID: "call_search", ToolName: "web_search", Arguments: json.RawMessage(`{"query":"ReAct"}`)},
		{Type: domain.BlockToolCall, ToolCallID: "call_read", ToolName: "file_read", Arguments: json.RawMessage(`{"path":"notes.md"}`)},
	}
	if err := turn.AddRuntimeEvent("run_parallel", domain.Event{Type: "assistant_blocks", Payload: map[string]any{"blocks": calls}}); err != nil {
		t.Fatalf("add native calls: %v", err)
	}
	results := []domain.ContentBlock{
		{Type: domain.BlockToolResult, ToolCallID: "call_search", Output: `{"title":"ReAct"}`},
		{Type: domain.BlockToolResult, ToolCallID: "call_read", Output: "local notes"},
	}
	if err := turn.AddRuntimeEvent("run_parallel", domain.Event{Type: "tool_results", Payload: map[string]any{"blocks": results}}); err != nil {
		t.Fatalf("add grouped results: %v", err)
	}
	if err := turn.AddAssistantText("comparison complete"); err != nil {
		t.Fatalf("add final: %v", err)
	}
	if err := turn.Commit(context.Background(), session.TurnCompleted, ""); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	store, err = session.NewStore(path, session.Config{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	view, err := store.BuildPrompt(context.Background(), "parallel-native", "continue")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	native := view.NativeHistory()
	if len(native) != 4 || native[1].Role != "assistant_blocks" || native[2].Role != "tool_results" {
		t.Fatalf("native history = %#v", native)
	}
	if len(native[1].Blocks) != 2 || len(native[2].Blocks) != 2 {
		t.Fatalf("grouping lost: calls=%#v results=%#v", native[1].Blocks, native[2].Blocks)
	}
	for index := range native[1].Blocks {
		if native[1].Blocks[index].ToolCallID != native[2].Blocks[index].ToolCallID {
			t.Fatalf("call/result mismatch at %d: %#v / %#v", index, native[1].Blocks[index], native[2].Blocks[index])
		}
	}
}

func TestCompactionRetainsRecentStructuredToolProtocol(t *testing.T) {
	store, err := session.NewStore(t.TempDir()+"/sessions.db", session.Config{TriggerBytes: 1_000_000, MinRecentTurns: 1})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	sessionID, _ := store.Ensure(context.Background(), "compact-structured")
	for index := 1; index <= 2; index++ {
		turn, beginErr := store.BeginTurn(context.Background(), sessionID, fmt.Sprintf("run_%d", index))
		if beginErr != nil {
			t.Fatalf("begin turn %d: %v", index, beginErr)
		}
		callID := fmt.Sprintf("call_%d", index)
		_ = turn.AddUser(fmt.Sprintf("question %d", index), nil)
		_ = turn.AddAssistantBlocks([]domain.ContentBlock{{Type: domain.BlockReasoning, ReasoningID: fmt.Sprintf("r_%d", index), EncryptedContent: fmt.Sprintf("cipher_%d", index)}})
		_ = turn.AddAssistantBlocks([]domain.ContentBlock{{Type: domain.BlockToolCall, ToolCallID: callID, ToolName: "web_search", Arguments: json.RawMessage(`{"query":"memory"}`)}})
		_ = turn.AddToolResults([]domain.ContentBlock{{Type: domain.BlockToolResult, ToolCallID: callID, Output: fmt.Sprintf("result %d", index)}})
		_ = turn.AddAssistantText(fmt.Sprintf("answer %d", index))
		_ = turn.SetMetrics(domain.RunMetrics{InputTokens: 10, OutputTokens: 2, TotalTokens: 12, ToolCalls: 1})
		if err := turn.Commit(context.Background(), session.TurnCompleted, ""); err != nil {
			t.Fatalf("commit turn %d: %v", index, err)
		}
	}
	view, err := store.ForcePrompt(context.Background(), sessionID, "current", 1)
	if err != nil {
		t.Fatalf("force prompt: %v", err)
	}
	if !view.Info.Compacted || len(view.History) != 6 {
		t.Fatalf("view = %#v history=%#v", view.Info, view.History)
	}
	native := view.NativeHistory()
	var call, result *domain.ContentBlock
	for messageIndex := range native {
		for blockIndex := range native[messageIndex].Blocks {
			block := &native[messageIndex].Blocks[blockIndex]
			if block.ToolCallID == "call_2" && block.Type == domain.BlockToolCall {
				call = block
			}
			if block.ToolCallID == "call_2" && block.Type == domain.BlockToolResult {
				result = block
			}
			if block.ToolCallID == "call_1" {
				t.Fatalf("old structured call leaked outside compacted digest: %#v", native)
			}
		}
	}
	if call == nil || result == nil || call.ToolCallID != result.ToolCallID {
		t.Fatalf("recent tool protocol not retained: %#v", native)
	}
}

func TestFailedTurnIsAuditableButNotReplayedByDefault(t *testing.T) {
	store, err := session.NewStore(t.TempDir()+"/sessions.db", session.Config{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	turn, err := store.BeginTurn(context.Background(), "failed-session", "run_failed")
	if err != nil {
		t.Fatalf("begin turn: %v", err)
	}
	_ = turn.AddUser("unfinished request", nil)
	_ = turn.AddAssistantBlocks([]domain.ContentBlock{{
		Type: domain.BlockToolCall, ToolCallID: "call_orphan", ToolName: "web_search", Arguments: json.RawMessage(`{"query":"unfinished"}`),
	}})
	_ = turn.SetMetrics(domain.RunMetrics{InputTokens: 9, OutputTokens: 1, TotalTokens: 10, ToolCalls: 1})
	if err := turn.Commit(context.Background(), session.TurnFailed, "provider failed"); err != nil {
		t.Fatalf("commit failed turn: %v", err)
	}
	messages, err := store.Messages(context.Background(), "failed-session")
	if err != nil || len(messages) != 0 {
		t.Fatalf("resume messages = %#v, err = %v", messages, err)
	}
	events, err := store.CanonicalEvents(context.Background(), "failed-session")
	if err != nil || len(events) != 4 {
		t.Fatalf("canonical events = %#v, err = %v", events, err)
	}
	status, err := store.Status(context.Background(), "failed-session")
	if err != nil || status.TurnCount != 1 || status.LastTurnStatus != string(session.TurnFailed) ||
		status.TotalTokens != 10 || status.MessageCount != 0 {
		t.Fatalf("failed status = %#v, err = %v", status, err)
	}
	view, err := store.BuildPrompt(context.Background(), "failed-session", "retry with a new request")
	if err != nil || len(view.NativeHistory()) != 0 {
		t.Fatalf("failed turn leaked into native history: %#v, err = %v", view.NativeHistory(), err)
	}
}

func TestFailedTurnCanExplicitlyRetainCompleteToolProtocol(t *testing.T) {
	store, err := session.NewStore(t.TempDir()+"/sessions.db", session.Config{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	turn, err := store.BeginTurn(context.Background(), "retained-failure", "run_failed")
	if err != nil {
		t.Fatalf("begin turn: %v", err)
	}
	_ = turn.AddUser("retain completed evidence", nil)
	_ = turn.AddAssistantBlocks([]domain.ContentBlock{{
		Type: domain.BlockToolCall, ToolCallID: "call_complete", ToolName: "web_search", Arguments: json.RawMessage(`{"query":"evidence"}`),
	}})
	_ = turn.AddToolResults([]domain.ContentBlock{{Type: domain.BlockToolResult, ToolCallID: "call_complete", Output: `{"ok":true}`}})
	if err := turn.CommitRetainingMessages(context.Background(), "report generation failed"); err != nil {
		t.Fatalf("retain failed turn: %v", err)
	}
	view, err := store.BuildPrompt(context.Background(), "retained-failure", "continue")
	if err != nil {
		t.Fatalf("build prompt: %v", err)
	}
	native := view.NativeHistory()
	if len(native) != 3 || native[1].Blocks[0].ToolCallID != "call_complete" || native[2].Blocks[0].ToolCallID != "call_complete" {
		t.Fatalf("retained native history = %#v", native)
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
