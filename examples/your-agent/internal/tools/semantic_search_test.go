package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemanticCodeSearchRanksDefinitionsAndReferences(t *testing.T) {
	root := t.TempDir()
	source := `package sample

func ResumePlanWithSession() {}

func caller() { ResumePlanWithSession() }
`
	if err := os.WriteFile(filepath.Join(root, "agent.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	definition := semanticCodeSearchDefinition(root)
	result, err := definition.Execute(context.Background(), map[string]any{
		"query": "ResumePlanWithSession", "action": "definition", "max_results": float64(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.(string)
	if !strings.Contains(text, "mode: local-symbol-index") || !strings.Contains(text, "symbol:ResumePlanWithSession") || !strings.Contains(text, "agent.go:3") {
		t.Fatalf("definition output:\n%s", text)
	}
	references, err := definition.Execute(context.Background(), map[string]any{
		"query": "ResumePlanWithSession", "action": "references",
	})
	if err != nil || !strings.Contains(references.(string), "ResumePlanWithSession()") {
		t.Fatalf("references=%v err=%v", references, err)
	}
}
