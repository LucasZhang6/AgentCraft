package tools_test

import (
	"context"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/tools"
)

func TestReadPaperCardAcceptsNormalizedTitle(t *testing.T) {
	registry := tools.NewRegistry()
	if err := tools.RegisterPaperTools(registry, []domain.Paper{{ID: "a-mem-agentic-memory", Title: "A-MEM: Agentic Memory for LLM Agents", Module: "Memory"}}); err != nil {
		t.Fatal(err)
	}
	result, err := registry.Execute(context.Background(), "read_paper_card", map[string]any{"id": "A-MEM: Agentic Memory for LLM Agents"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result.(domain.Paper).Module != "Memory" {
		t.Fatalf("result = %#v", result.Result)
	}
}
