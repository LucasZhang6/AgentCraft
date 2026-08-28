package model

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/provider"
)

type sequenceClient struct {
	responses []provider.Response
	requests  []provider.Request
	index     int
}

func (client *sequenceClient) Generate(_ context.Context, request provider.Request) (provider.Response, error) {
	client.requests = append(client.requests, request)
	response := client.responses[client.index]
	client.index++
	if request.OnDelta != nil {
		request.OnDelta(response.Text)
	}
	return response, nil
}

func TestRunStepUsesProviderNativeTools(t *testing.T) {
	client := &sequenceClient{responses: []provider.Response{{Blocks: []domain.ContentBlock{{
		Type: domain.BlockToolCall, ToolCallID: "call_1", ToolName: "search_papers",
		Arguments: json.RawMessage(`{"query":"memory"}`),
	}}}}}
	model, err := NewOpenAIModel(OpenAIModelConfig{Client: client, Model: "compatible"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := model.RunStep(context.Background(), domain.StepContext{
		Goal: "explain", Step: domain.PlanStep{ID: "search", Tool: "search_papers"},
		Images: []string{"data:image/png;base64,aGVsbG8="}, Tools: []domain.ToolDescription{{Name: "search_papers"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Blocks) != 1 || response.Blocks[0].ToolCallID != "call_1" {
		t.Fatalf("response = %#v", response)
	}
	if len(client.requests) != 1 || len(client.requests[0].Tools) != 1 || client.requests[0].Tools[0].Name != "search_papers" {
		t.Fatalf("native tools request = %#v", client.requests)
	}
	if len(client.requests[0].Images) != 1 || strings.Contains(client.requests[0].Input, "data:image") || strings.Contains(client.requests[0].Input, `"tools"`) {
		t.Fatalf("native fields were duplicated into JSON input: %#v", client.requests[0])
	}
}

func TestGenerateFinalDoesNotExposeTools(t *testing.T) {
	client := &sequenceClient{responses: []provider.Response{{
		Text: "## 问题背景\n证据\n## 核心方法\n方法\n## 工程启发\n启发\n## 局限\n局限 https://example.test/paper",
	}}}
	model, err := NewOpenAIModel(OpenAIModelConfig{Client: client, Model: "compatible"})
	if err != nil {
		t.Fatal(err)
	}
	response, err := model.GenerateFinal(context.Background(), domain.FinalContext{Goal: "explain"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content == "" || len(client.requests) != 1 || len(client.requests[0].Tools) != 0 {
		t.Fatalf("response=%#v requests=%#v", response, client.requests)
	}
}
