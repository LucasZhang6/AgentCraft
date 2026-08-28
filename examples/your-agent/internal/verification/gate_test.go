package verification

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/planning"
)

func TestGateAppendsPersistedVerificationStepAfterWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/gate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := planning.NewStore(filepath.Join(root, "plans.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	item, err := store.Save(context.Background(), "session", "run", "change code", []domain.PlanStep{{
		ID: "write", Tool: "file_write", Description: "write", SuccessCriteria: "file changed", Status: domain.PlanCompleted,
	}})
	if err != nil {
		t.Fatal(err)
	}
	gate := New(root)
	gate.ObserveTool(context.Background(), "file_write", map[string]any{"path": "main.go"}, domain.ToolExecution{}, nil)

	updated, injected, err := gate.Ensure(context.Background(), store, item)
	if err != nil {
		t.Fatal(err)
	}
	if !injected || updated.Status == domain.PlanCompleted || len(updated.Steps) != 2 {
		t.Fatalf("updated plan = %#v injected=%v", updated, injected)
	}
	step := updated.Steps[1]
	if step.Tool != "bash" || step.AgentRole != "verifier" || step.Dependencies[0] != "write" {
		t.Fatalf("verification step = %#v", step)
	}
	if gate.Snapshot().Command != "go test ./..." {
		t.Fatalf("gate snapshot = %#v", gate.Snapshot())
	}
}

func TestGateAcceptsSuccessfulVerificationCommand(t *testing.T) {
	gate := New(t.TempDir())
	gate.ObserveTool(context.Background(), "file_edit", map[string]any{"path": "README.md"}, domain.ToolExecution{}, nil)
	gate.ObserveTool(context.Background(), "bash", map[string]any{"command": "git diff --check"}, domain.ToolExecution{}, nil)
	if snapshot := gate.Snapshot(); !snapshot.MaterialWork || !snapshot.Verified {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
