package model

import (
	"context"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/provider"
)

type sequenceClient struct {
	responses []provider.Response
	index     int
}

func (client *sequenceClient) Generate(_ context.Context, request provider.Request) (provider.Response, error) {
	response := client.responses[client.index]
	client.index++
	if request.OnDelta != nil {
		request.OnDelta(response.Text)
	}
	return response, nil
}

func TestDecisionToleratesCompatibleProviderPaperExtensions(t *testing.T) {
	client := &sequenceClient{responses: []provider.Response{
		{Text: `{"type":"final","paper":{"title":"Agent Memory","problem":{"summary":"nested extension"}},"core_engineering_implications":["lifecycle"]}`},
		{Text: "## 问题背景\n证据\n## 核心方法\n方法\n## 工程启发\n启发\n## 局限\n局限 https://example.test/paper"},
	}}
	model, err := NewOpenAIModel(OpenAIModelConfig{Client: client, Model: "compatible"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := model.Decide(context.Background(), domain.DecisionContext{Goal: "explain"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Decision.Type != domain.DecisionFinal || response.Decision.Paper != nil || response.Decision.Content == "" {
		t.Fatalf("decision = %#v", response.Decision)
	}
}

func TestDecisionConsumesFirstOfMultipleJSONObjects(t *testing.T) {
	decision, err := decodeDecision(`{"type":"tool","tool":"search_papers","args":{"query":"memory"}}
{"explanation":"extra provider payload"}`)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Type != domain.DecisionTool || decision.Tool != "search_papers" || decision.Args["query"] != "memory" {
		t.Fatalf("decision = %#v", decision)
	}
}
