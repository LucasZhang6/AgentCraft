package model

import (
	"context"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
)

func TestDemoModelSearchesLatestSessionRequest(t *testing.T) {
	response, err := (DemoModel{}).Decide(context.Background(), domain.DecisionContext{
		Goal: `[Session context]

User:
解读 Agent Memory 的代表性论文

Assistant:
A-MEM 研究动态记忆。

User:
解读 Tool Use 的代表性论文`,
	})
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	query, _ := response.Decision.Args["query"].(string)
	if query != "解读 Tool Use 的代表性论文" {
		t.Fatalf("search query = %q", query)
	}
}
