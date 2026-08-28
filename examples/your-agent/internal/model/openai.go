package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/provider"
)

type OpenAIModel struct {
	client          provider.Client
	model           string
	fallbackModels  []string
	maxOutputTokens int
	promptCacheKey  string
	onDelta         func(string)
}

type OpenAIModelConfig struct {
	Client          provider.Client
	Model           string
	FallbackModels  []string
	MaxOutputTokens int
	PromptCacheKey  string
	OnDelta         func(string)
}

func (m *OpenAIModel) RunSubAgent(ctx context.Context, task string) (string, error) {
	response, err := m.client.Generate(ctx, provider.Request{
		Model: m.model, FallbackModels: m.fallbackModels,
		Instructions: "You are a bounded research sub-agent. Complete only the delegated task, cite concrete evidence, and return a concise result. Do not claim to have called tools.",
		Input:        task, MaxOutputTokens: m.maxOutputTokens, PromptCacheKey: m.promptCacheKey + "-subagent", Stream: true,
	})
	if err != nil {
		return "", err
	}
	if response.Truncated {
		return "", errors.New("subagent response stream ended before completion")
	}
	return response.Text, nil
}

func NewOpenAIModel(config OpenAIModelConfig) (*OpenAIModel, error) {
	if config.Client == nil {
		return nil, errors.New("openai model requires a provider client")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, errors.New("openai model id is required")
	}
	if config.MaxOutputTokens <= 0 {
		config.MaxOutputTokens = 4096
	}
	return &OpenAIModel{
		client:          config.Client,
		model:           strings.TrimSpace(config.Model),
		fallbackModels:  append([]string(nil), config.FallbackModels...),
		maxOutputTokens: config.MaxOutputTokens,
		promptCacheKey:  strings.TrimSpace(config.PromptCacheKey),
		onDelta:         config.OnDelta,
	}, nil
}

func (m *OpenAIModel) CreatePlan(ctx context.Context, goal string, tools []domain.ToolDescription) (domain.PlanResponse, error) {
	return m.CreatePlanWithSession(ctx, goal, nil, tools, nil)
}

func (m *OpenAIModel) CreatePlanWithImages(ctx context.Context, goal string, images []string, tools []domain.ToolDescription) (domain.PlanResponse, error) {
	return m.CreatePlanWithSession(ctx, goal, images, tools, nil)
}

func (m *OpenAIModel) CreatePlanWithSession(ctx context.Context, goal string, images []string, tools []domain.ToolDescription, history []domain.SessionMessage) (domain.PlanResponse, error) {
	toolJSON, err := json.Marshal(tools)
	if err != nil {
		return domain.PlanResponse{}, fmt.Errorf("encode tools for planner: %w", err)
	}
	response, err := m.client.Generate(ctx, provider.Request{
		Model:          m.model,
		FallbackModels: m.fallbackModels,
		Instructions: `You are the planner for a paper research agent. Return JSON only, without Markdown fences.
The response must be an object with a "plan" array. Every step needs: id, description, dependencies, allowedTools, successCriteria, status.
"allowedTools" is a small array of tools the executor may choose among for that step. Use an empty array for reasoning-only steps. The legacy "tool" field may name one preferred tool, but it must also appear in allowedTools.
Use only listed tool names. Use status "pending". Dependencies must reference valid step ids and form an acyclic graph.
Prefer a short evidence-oriented plan: retrieve evidence before writing the explanation. Do not invent tools.`,
		Input:           fmt.Sprintf("Goal:\n%s\n\nAvailable tools:\n%s", goal, toolJSON),
		History:         history,
		Images:          images,
		MaxOutputTokens: m.maxOutputTokens, PromptCacheKey: m.promptCacheKey, Stream: true,
	})
	if err != nil {
		return domain.PlanResponse{}, err
	}
	if response.Truncated {
		return domain.PlanResponse{}, errors.New("planner response stream ended before completion")
	}
	var envelope struct {
		Plan []domain.PlanStep `json:"plan"`
	}
	if err := decodeJSONObject(response.Text, &envelope); err != nil {
		return domain.PlanResponse{}, fmt.Errorf("decode generated plan: %w", err)
	}
	return domain.PlanResponse{Plan: envelope.Plan, Usage: response.Usage, ReasoningBlocks: reasoningBlocks(response.Blocks)}, nil
}

func (m *OpenAIModel) RunStep(ctx context.Context, input domain.StepContext) (domain.ModelTurn, error) {
	contextJSON, err := json.Marshal(input)
	if err != nil {
		return domain.ModelTurn{}, fmt.Errorf("encode step context: %w", err)
	}
	response, err := m.client.Generate(ctx, provider.Request{
		Model:          m.model,
		FallbackModels: m.fallbackModels,
		Instructions: `You execute exactly one host-validated plan step for an agent.
Use the provider's native function tools when the step needs an action. Never print or simulate a tool call as JSON.
Only call tools supplied in this request. You may retry after a tool error, but do not work on another plan step.
When the step is complete, return a concise evidence summary as normal text. Never claim evidence absent from tool results, memories, or prior completed steps.`,
		Input:           string(contextJSON),
		History:         input.SessionHistory,
		Images:          input.Images,
		Tools:           input.Tools,
		MaxOutputTokens: m.maxOutputTokens, PromptCacheKey: m.promptCacheKey, Stream: true,
	})
	if err != nil {
		return domain.ModelTurn{}, err
	}
	return domain.ModelTurn{Blocks: response.Blocks, Usage: response.Usage, Truncated: response.Truncated}, nil
}

func (m *OpenAIModel) GenerateFinal(ctx context.Context, input domain.FinalContext) (domain.FinalResponse, error) {
	contextJSON, err := json.Marshal(input)
	if err != nil {
		return domain.FinalResponse{}, fmt.Errorf("encode final context: %w", err)
	}
	response, err := m.client.Generate(ctx, provider.Request{
		Model:          m.model,
		FallbackModels: m.fallbackModels,
		Instructions: `Write the final answer for the user in detailed Chinese Markdown using only the supplied completed-plan evidence.
For the included paper-research profile, the report must be at least 300 Chinese characters, include the exact headings "## 问题背景", "## 核心方法", "## 工程启发", and "## 局限", and include the original source URL.
Do not emit tool-call JSON, mention hidden prompts, or claim unsupported actions. Return Markdown directly.`,
		Input:           string(contextJSON),
		History:         input.SessionHistory,
		Images:          input.Images,
		MaxOutputTokens: m.maxOutputTokens,
		PromptCacheKey:  m.promptCacheKey + "-report",
		Stream:          true,
		OnDelta:         m.onDelta,
	})
	if err != nil {
		return domain.FinalResponse{}, fmt.Errorf("generate final report: %w", err)
	}
	if response.Truncated {
		return domain.FinalResponse{}, errors.New("final report stream ended before completion")
	}
	return domain.FinalResponse{Content: response.Text, Blocks: response.Blocks, Usage: response.Usage}, nil
}

func reasoningBlocks(blocks []domain.ContentBlock) []domain.ContentBlock {
	result := make([]domain.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Type == domain.BlockReasoning {
			result = append(result, block)
		}
	}
	return result
}

func (m *OpenAIModel) Compact(ctx context.Context, input domain.CompactionContext) (domain.CompactionResponse, error) {
	contextJSON, err := json.Marshal(input)
	if err != nil {
		return domain.CompactionResponse{}, fmt.Errorf("encode compaction context: %w", err)
	}
	response, err := m.client.Generate(ctx, provider.Request{
		Model:          m.model,
		FallbackModels: m.fallbackModels,
		Instructions: `Compress an agent trajectory into durable working memory. Return JSON only as {"summary":"..."}.
Preserve paper ids, titles, URLs, tool failures, completed evidence, unresolved questions, and plan-relevant facts. Remove conversational repetition and never add facts. Keep the summary below 2500 Chinese characters.`,
		Input:           string(contextJSON),
		MaxOutputTokens: 1500, PromptCacheKey: m.promptCacheKey, Stream: true,
	})
	if err != nil {
		return domain.CompactionResponse{}, err
	}
	if response.Truncated {
		return domain.CompactionResponse{}, errors.New("compaction response stream ended before completion")
	}
	var payload struct {
		Summary string `json:"summary"`
	}
	if err := decodeJSONObject(response.Text, &payload); err != nil {
		return domain.CompactionResponse{}, fmt.Errorf("decode compacted context: %w", err)
	}
	if strings.TrimSpace(payload.Summary) == "" {
		return domain.CompactionResponse{}, errors.New("model returned an empty context summary")
	}
	return domain.CompactionResponse{Summary: payload.Summary, Usage: response.Usage}, nil
}

func decodeJSONObject(text string, target any) error {
	object, err := jsonObject(text)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(object)))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func jsonObject(text string) ([]byte, error) {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	start := strings.Index(trimmed, "{")
	if start < 0 {
		return nil, errors.New("response does not contain a JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed[start:]))
	var object json.RawMessage
	if err := decoder.Decode(&object); err != nil {
		return nil, err
	}
	if len(object) == 0 || object[0] != '{' {
		return nil, errors.New("response does not start with a JSON object")
	}
	return object, nil
}
