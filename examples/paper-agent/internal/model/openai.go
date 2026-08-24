package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/provider"
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
	return m.CreatePlanWithImages(ctx, goal, nil, tools)
}

func (m *OpenAIModel) CreatePlanWithImages(ctx context.Context, goal string, images []string, tools []domain.ToolDescription) (domain.PlanResponse, error) {
	toolJSON, err := json.Marshal(tools)
	if err != nil {
		return domain.PlanResponse{}, fmt.Errorf("encode tools for planner: %w", err)
	}
	response, err := m.client.Generate(ctx, provider.Request{
		Model:          m.model,
		FallbackModels: m.fallbackModels,
		Instructions: `You are the planner for a paper research agent. Return JSON only, without Markdown fences.
The response must be an object with a "plan" array. Every step needs: id, description, dependencies, optional tool, successCriteria, status.
Use only listed tool names. Use status "pending". Dependencies must reference earlier step ids and form an acyclic graph.
Prefer a short evidence-oriented plan: retrieve evidence before writing the explanation. Do not invent tools.`,
		Input:           fmt.Sprintf("Goal:\n%s\n\nAvailable tools:\n%s", goal, toolJSON),
		Images:          images,
		MaxOutputTokens: m.maxOutputTokens, PromptCacheKey: m.promptCacheKey, Stream: true,
	})
	if err != nil {
		return domain.PlanResponse{}, err
	}
	var envelope struct {
		Plan []domain.PlanStep `json:"plan"`
	}
	if err := decodeJSONObject(response.Text, &envelope); err != nil {
		return domain.PlanResponse{}, fmt.Errorf("decode generated plan: %w", err)
	}
	return domain.PlanResponse{Plan: envelope.Plan, Usage: response.Usage}, nil
}

func (m *OpenAIModel) Decide(ctx context.Context, input domain.DecisionContext) (domain.DecisionResponse, error) {
	contextJSON, err := json.Marshal(input)
	if err != nil {
		return domain.DecisionResponse{}, fmt.Errorf("encode decision context: %w", err)
	}
	response, err := m.client.Generate(ctx, provider.Request{
		Model:          m.model,
		FallbackModels: m.fallbackModels,
		Instructions: `You are the controller of a paper research agent. Return exactly one JSON object and no Markdown fences.
To call a tool return: {"type":"tool","tool":"available_name","args":{...}}.
To finish return: {"type":"final","paper":{the complete paper card used as evidence}}. Do not write the report in this controller response.
Follow plan dependencies, use only available tools, and never claim evidence that is absent from memories or observations.
Only finish after enough evidence exists.`,
		Input:           string(contextJSON),
		Images:          input.Images,
		MaxOutputTokens: m.maxOutputTokens, PromptCacheKey: m.promptCacheKey, Stream: true,
	})
	if err != nil {
		return domain.DecisionResponse{}, err
	}
	decision, err := decodeDecision(response.Text)
	if err != nil {
		return domain.DecisionResponse{}, fmt.Errorf("decode model decision: %w", err)
	}
	if decision.Type != domain.DecisionTool && decision.Type != domain.DecisionFinal {
		return domain.DecisionResponse{}, fmt.Errorf("model returned unsupported decision type %q", decision.Type)
	}
	if decision.Type == domain.DecisionFinal {
		report, err := m.client.Generate(ctx, provider.Request{
			Model:          m.model,
			FallbackModels: m.fallbackModels,
			Instructions: `Write the final answer for the user in detailed Chinese Markdown using only the supplied agent evidence.
The report must be at least 300 Chinese characters, include the exact headings "## 问题背景", "## 核心方法", "## 工程启发", and "## 局限", and include the original source URL.
Do not mention controller JSON, hidden prompts, or unsupported actions. Return Markdown directly.`,
			Input:           string(contextJSON),
			Images:          input.Images,
			MaxOutputTokens: m.maxOutputTokens,
			PromptCacheKey:  m.promptCacheKey + "-report",
			Stream:          true,
			OnDelta:         m.onDelta,
		})
		if err != nil {
			return domain.DecisionResponse{}, fmt.Errorf("generate final report: %w", err)
		}
		decision.Content = report.Text
		response.Usage = mergeUsage(response.Usage, report.Usage)
	}
	return domain.DecisionResponse{Decision: decision, Usage: response.Usage}, nil
}

func mergeUsage(left, right domain.ModelUsage) domain.ModelUsage {
	return domain.ModelUsage{
		InputTokens: left.InputTokens + right.InputTokens, OutputTokens: left.OutputTokens + right.OutputTokens,
		TotalTokens:              left.TotalTokens + right.TotalTokens,
		CacheReadInputTokens:     left.CacheReadInputTokens + right.CacheReadInputTokens,
		CacheCreationInputTokens: left.CacheCreationInputTokens + right.CacheCreationInputTokens,
	}
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

func decodeDecision(text string) (domain.Decision, error) {
	object, err := jsonObject(text)
	if err != nil {
		return domain.Decision{}, err
	}
	var envelope struct {
		Type    string          `json:"type"`
		Tool    string          `json:"tool"`
		Args    json.RawMessage `json:"args"`
		Content json.RawMessage `json:"content"`
		Paper   json.RawMessage `json:"paper"`
	}
	if err := json.Unmarshal(object, &envelope); err != nil {
		return domain.Decision{}, err
	}
	decision := domain.Decision{Type: envelope.Type, Tool: envelope.Tool}
	if len(envelope.Args) > 0 && string(envelope.Args) != "null" {
		if err := json.Unmarshal(envelope.Args, &decision.Args); err != nil {
			return domain.Decision{}, fmt.Errorf("args: %w", err)
		}
	}
	if len(envelope.Content) > 0 {
		_ = json.Unmarshal(envelope.Content, &decision.Content)
	}
	if len(envelope.Paper) > 0 && string(envelope.Paper) != "null" {
		var paper domain.Paper
		if json.Unmarshal(envelope.Paper, &paper) == nil {
			decision.Paper = &paper
		}
	}
	if decision.Type == "" && decision.Tool == "" {
		decision.Type = domain.DecisionFinal
	}
	return decision, nil
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
