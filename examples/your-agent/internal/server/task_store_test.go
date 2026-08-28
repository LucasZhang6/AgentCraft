package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestTaskStoreRestoresAndInterruptsActiveTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.db")
	store, err := newTaskStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.save(context.Background(), taskRecord{
		ID: "task-1", SessionID: "session-1", Status: "running", Messages: []string{"started"}, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}

	store, err = newTaskStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	if err := store.interruptActive(context.Background()); err != nil {
		t.Fatal(err)
	}
	records, err := store.list(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != "interrupted" || !records[0].Complete || records[0].Success {
		t.Fatalf("unexpected restored record: %+v", records)
	}
}
