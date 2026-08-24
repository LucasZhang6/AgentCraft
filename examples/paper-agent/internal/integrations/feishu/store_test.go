package feishu

import "testing"

func TestStoreMappingAndEventDedup(t *testing.T) {
	store, err := NewStore(t.TempDir() + "/feishu.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first, err := store.MarkEvent("evt1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.MarkEvent("evt1")
	if err != nil {
		t.Fatal(err)
	}
	if !first || second {
		t.Fatalf("dedup first=%v second=%v, want true false", first, second)
	}

	err = store.UpsertMapping(Mapping{Key: "tenant:chat", TenantKey: "tenant", ChatID: "chat", SessionID: "sess", LastTaskID: "task"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetMapping("tenant:chat")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SessionID != "sess" || got.LastTaskID != "task" {
		t.Fatalf("mapping = %+v", got)
	}
	if err := store.ClearSession("tenant:chat"); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetMapping("tenant:chat")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SessionID != "" || got.LastTaskID != "" {
		t.Fatalf("mapping after clear = %+v", got)
	}
}
