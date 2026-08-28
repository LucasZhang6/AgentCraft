package session

import (
	"context"
	"strings"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

func TestCanonicalTurnCommitRollsBackEveryProjection(t *testing.T) {
	store, err := NewStore(t.TempDir()+"/sessions.db", Config{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	turn, err := store.BeginTurn(context.Background(), "rollback", "run_rollback")
	if err != nil {
		t.Fatalf("begin turn: %v", err)
	}
	_ = turn.AddUser("must roll back", nil)
	_ = turn.AddAssistantText("not committed")
	_ = turn.SetMetrics(domain.RunMetrics{InputTokens: 7, OutputTokens: 3, TotalTokens: 10})
	if _, err := store.db.Exec(`
CREATE TRIGGER fail_turn_status BEFORE INSERT ON session_turn_events
WHEN NEW.kind = 'turn_status'
BEGIN SELECT RAISE(ABORT, 'forced status failure'); END;`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := turn.Commit(context.Background(), TurnCompleted, ""); err == nil {
		t.Fatal("commit unexpectedly succeeded")
	}
	for _, table := range []string{"session_turns", "session_turn_events", "session_turn_metrics", "session_messages", "session_events"} {
		var count int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE session_id = ?`, "rollback").Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s contains %d partial rows", table, count)
		}
	}
}

func TestReActContinuationRuntimeEventRestoresNativeUserMessage(t *testing.T) {
	store, err := NewStore(t.TempDir()+"/sessions.db", Config{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	turn, err := store.BeginTurn(context.Background(), "continuation", "run_continuation")
	if err != nil {
		t.Fatal(err)
	}
	note := "continue after complete tool blocks"
	if err := turn.AddRuntimeEvent("run_continuation", domain.Event{
		Type: "react_user_message", Payload: map[string]any{"reason": "stream_truncated", "text": note},
	}); err != nil {
		t.Fatal(err)
	}
	if err := turn.Commit(context.Background(), TurnCompleted, ""); err != nil {
		t.Fatal(err)
	}
	messages, err := store.Messages(context.Background(), "continuation")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != "user" || len(messages[0].Blocks) != 1 || !strings.Contains(messages[0].Blocks[0].Text, note) {
		t.Fatalf("restored messages = %#v", messages)
	}
}
