package goal_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/goal"
)

func TestGoalLifecyclePersistsAcrossStoreInstances(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "goals.db")
	store, err := goal.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Set(ctx, "session-1", "ship a reliable agent")
	if err != nil {
		t.Fatal(err)
	}
	if created.State != goal.Pursuing || created.Iterations != 0 {
		t.Fatalf("unexpected created goal: %#v", created)
	}
	if _, err := store.Pause(ctx, "session-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resume(ctx, "session-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordProgress(ctx, "session-1", 120, 2, "two turns complete"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := goal.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.Get(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if !persisted.AutoResume || persisted.TokensUsed != 120 || persisted.Iterations != 2 {
		t.Fatalf("goal did not persist: %#v", persisted)
	}
	if _, err := reopened.Achieve(ctx, "session-1", 180, 3, "verified"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Clear(ctx, "session-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Get(ctx, "session-1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Get after clear error = %v, want sql.ErrNoRows", err)
	}
}

func TestGoalContinuationPromptRetainsObjective(t *testing.T) {
	item := goal.Goal{Objective: "finish the same task", State: goal.Pursuing, Iterations: 4}
	prompt := item.ContinuationPrompt("verify the release")
	for _, want := range []string{"finish the same task", "verify the release", "Do not restart"} {
		if !contains(prompt, want) {
			t.Fatalf("prompt %q does not contain %q", prompt, want)
		}
	}
}

func contains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}
