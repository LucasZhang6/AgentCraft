package memory_test

import (
	"context"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/memory"
)

func TestStoreRetrievesByScopeBudgetAndArchives(t *testing.T) {
	store, err := memory.NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	userMemory, err := store.Remember(ctx, domain.MemoryInput{
		Key: "explanation_style", Value: "prefer concise Chinese explanations", Source: "user-confirmed", Scope: "user", Confidence: 1,
	})
	if err != nil {
		t.Fatalf("remember user: %v", err)
	}
	if _, err := store.Remember(ctx, domain.MemoryInput{
		Key: "project_topic", Value: "agent memory retrieval", Source: "project", Scope: "project", Confidence: 0.8,
	}); err != nil {
		t.Fatalf("remember project: %v", err)
	}

	items, err := store.Retrieve(ctx, domain.MemoryQuery{Text: "explanation style", Scopes: []string{"user"}, Limit: 1, LimitBytes: 300})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(items) != 1 || items[0].ID != userMemory.ID {
		t.Fatalf("items = %#v", items)
	}
	if err := store.Forget(ctx, userMemory.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	items, err = store.Retrieve(ctx, domain.MemoryQuery{Scopes: []string{"user"}})
	if err != nil {
		t.Fatalf("retrieve archived: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("archived memory was retrieved: %#v", items)
	}
}

func TestStoreRejectsSensitiveMemory(t *testing.T) {
	store, err := memory.NewStore(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	_, err = store.Remember(context.Background(), domain.MemoryInput{
		Key: "credential", Value: "api_key=sk-this-should-never-be-saved", Source: "tool", Scope: "user",
	})
	if err == nil {
		t.Fatal("expected sensitive memory rejection")
	}
}
