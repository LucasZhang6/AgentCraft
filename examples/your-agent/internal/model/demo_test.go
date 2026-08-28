package model

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

func TestDemoModelSearchesLatestSessionRequest(t *testing.T) {
	response, err := (DemoModel{}).RunStep(context.Background(), domain.StepContext{
		Goal: `[Session context]

User:
解读 Agent Memory 的代表性论文

Assistant:
A-MEM 研究动态记忆。

User:
		解读 Tool Use 的代表性论文`,
		Step: domain.PlanStep{ID: "search", Tool: "search_papers"},
	})
	if err != nil {
		t.Fatalf("run step: %v", err)
	}
	if len(response.Blocks) != 1 || response.Blocks[0].Type != domain.BlockToolCall {
		t.Fatalf("blocks = %#v", response.Blocks)
	}
	var args map[string]any
	if err := json.Unmarshal(response.Blocks[0].Arguments, &args); err != nil {
		t.Fatal(err)
	}
	query, _ := args["query"].(string)
	if query != "解读 Tool Use 的代表性论文" {
		t.Fatalf("search query = %q", query)
	}
}
