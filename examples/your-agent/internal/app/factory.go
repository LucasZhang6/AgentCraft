package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/agent"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/evaluator"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/memory"
	metricsstore "github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/metrics"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/model"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/planning"
	llmprovider "github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/provider"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/session"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/skills"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/subagent"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/tools"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/trajectory"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/verification"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/knowledge"
)

type Config struct {
	DataDir               string
	SessionID             string
	WorkDir               string
	MaxSteps              int
	MaxGoalTurns          int
	TokenBudget           int
	MaxRecentObservations int
	Provider              string
	Model                 string
	FallbackModels        []string
	APIKey                string
	BaseURL               string
	MaxOutputTokens       int
	HTTPClient            *http.Client
	ToolTimeout           time.Duration
	ToolOutputBytes       int
	Approval              tools.ApprovalFunc
	Clarify               tools.ClarifyFunc
	OnEvent               trajectory.OnEvent
	OnStream              func(string)
	SkillPaths            []string
	SubAgents             *subagent.Manager
}

type Runtime struct {
	Agent         *agent.YourAgent
	RunID         string
	LogPath       string
	MemoryPath    string
	MetricsPath   string
	PlansPath     string
	SubagentsPath string
	memory        *memory.Store
	metrics       *metricsstore.Store
	plans         *planning.Store
	subagents     *subagent.Manager
	ownSubagents  bool
}

func New(config Config) (Runtime, error) {
	if config.DataDir == "" {
		config.DataDir = ".agent-data"
	}
	if config.MaxSteps <= 0 {
		config.MaxSteps = 6
	}
	if config.MaxGoalTurns < 0 {
		config.MaxGoalTurns = 0
	}
	if config.MaxRecentObservations <= 0 {
		config.MaxRecentObservations = 4
	}
	if strings.TrimSpace(config.WorkDir) == "" {
		config.WorkDir = "."
	}
	if strings.TrimSpace(config.SessionID) == "" {
		config.SessionID = "standalone"
	}
	papers, err := tools.LoadCatalog(knowledge.PapersJSON)
	if err != nil {
		return Runtime{}, err
	}
	verificationGate := verification.New(config.WorkDir)
	skillManager := skills.NewManager(config.WorkDir, config.SkillPaths...)
	if err := skillManager.LoadAll(); err != nil {
		return Runtime{}, fmt.Errorf("load skills: %w", err)
	}
	if config.OnEvent != nil {
		for _, warning := range skillManager.Warnings() {
			config.OnEvent(domain.Event{Timestamp: time.Now().UTC(), Type: "skill_warning", Payload: warning})
		}
	}
	registry := tools.NewRegistry(tools.RegistryOptions{
		DefaultTimeout: config.ToolTimeout,
		MaxOutputBytes: config.ToolOutputBytes,
		Approval:       config.Approval,
		Observer:       verificationGate,
	})
	if err := tools.RegisterPaperTools(registry, papers); err != nil {
		return Runtime{}, fmt.Errorf("register paper tools: %w", err)
	}
	runID, err := newRunID()
	if err != nil {
		return Runtime{}, err
	}
	logPath := filepath.Join(config.DataDir, "runs", runID+".jsonl")
	memoryPath := filepath.Join(config.DataDir, "memory.db")
	memoryStore, err := memory.NewStore(memoryPath)
	if err != nil {
		return Runtime{}, err
	}
	metricsPath := filepath.Join(config.DataDir, "metrics.db")
	metricsStore, err := metricsstore.NewStore(metricsPath)
	if err != nil {
		memoryStore.Close()
		return Runtime{}, err
	}
	plansPath := filepath.Join(config.DataDir, "plans.db")
	plansStore, err := planning.NewStore(plansPath)
	if err != nil {
		memoryStore.Close()
		metricsStore.Close()
		return Runtime{}, err
	}

	var runtimeModel agent.Model
	switch strings.ToLower(strings.TrimSpace(config.Provider)) {
	case "", "demo":
		runtimeModel = model.DemoModel{}
	case "openai":
		client := llmprovider.NewOpenAIClient(llmprovider.OpenAIConfig{
			BaseURL: config.BaseURL, APIKey: config.APIKey, UserAgent: "your-agent/0.2",
			HTTPClient: config.HTTPClient, MaxRetries: 2,
		})
		openAIModel, err := model.NewOpenAIModel(model.OpenAIModelConfig{
			Client: client, Model: config.Model, FallbackModels: config.FallbackModels,
			MaxOutputTokens: config.MaxOutputTokens, PromptCacheKey: "your-agent-runtime-v1", OnDelta: config.OnStream,
		})
		if err != nil {
			memoryStore.Close()
			metricsStore.Close()
			plansStore.Close()
			return Runtime{}, err
		}
		runtimeModel = openAIModel
	default:
		memoryStore.Close()
		metricsStore.Close()
		plansStore.Close()
		return Runtime{}, fmt.Errorf("unsupported provider %q; use demo or openai", config.Provider)
	}
	var childRuntime *subagent.ToolRuntime
	subAgent := tools.SubAgentFunc(func(ctx context.Context, task string) (string, error) {
		if childRuntime == nil {
			return "", errors.New("tool-enabled subagent runtime is not initialized")
		}
		return childRuntime.Run(ctx, task)
	})
	subagentsPath := filepath.Join(config.DataDir, "subagents.db")
	subagents := config.SubAgents
	ownSubagents := false
	if subagents == nil {
		subagents, err = subagent.NewManager(subagentsPath)
		if err != nil {
			memoryStore.Close()
			metricsStore.Close()
			plansStore.Close()
			return Runtime{}, fmt.Errorf("initialize subagent lifecycle: %w", err)
		}
		ownSubagents = true
	}
	if err := tools.RegisterRuntimeTools(registry, tools.RuntimeOptions{
		WorkDir: config.WorkDir, HTTPClient: config.HTTPClient, Clarify: config.Clarify,
		SubAgents: subagents, SubAgent: subAgent, SessionID: config.SessionID, RunID: runID, Skills: skillManager,
	}); err != nil {
		memoryStore.Close()
		metricsStore.Close()
		plansStore.Close()
		if ownSubagents {
			_ = subagents.Close()
		}
		return Runtime{}, fmt.Errorf("register runtime tools: %w", err)
	}
	if warnings, err := tools.RegisterExternalTools(context.Background(), registry, config.WorkDir); err != nil {
		memoryStore.Close()
		metricsStore.Close()
		plansStore.Close()
		if ownSubagents {
			_ = subagents.Close()
		}
		return Runtime{}, fmt.Errorf("register external tools: %w", err)
	} else if config.OnEvent != nil {
		for _, warning := range warnings {
			config.OnEvent(domain.Event{Timestamp: time.Now().UTC(), Type: "plugin_warning", Payload: warning})
		}
	}
	childRuntime, err = subagent.NewToolRuntime(runtimeModel, registry, max(config.MaxSteps*2, 12))
	if err != nil {
		memoryStore.Close()
		metricsStore.Close()
		plansStore.Close()
		if ownSubagents {
			_ = subagents.Close()
		}
		return Runtime{}, fmt.Errorf("initialize tool-enabled subagent runtime: %w", err)
	}

	yourAgent, err := agent.New(agent.Config{
		Model: runtimeModel, Tools: registry, Memory: memoryStore, Plans: planning.Validator{},
		PlanStore:    plansStore,
		Scheduler:    planning.Scheduler{Store: plansStore, Verifier: planning.Verifier{WorkDir: config.WorkDir}, Concurrency: 4},
		Verification: verificationGate,
		Evaluator:    evaluator.ReportEvaluator{}, Logger: trajectory.NewLogger(logPath, config.OnEvent),
		MetricsRecorder: metricsStore.Bind(runID),
		SessionID:       config.SessionID, RunID: runID,
		MaxSteps: config.MaxSteps, MaxGoalTurns: config.MaxGoalTurns,
		TokenBudget: config.TokenBudget, MaxRecentObservations: config.MaxRecentObservations,
		SkillPrompt: skillManager.FormatForPrompt(),
	})
	if err != nil {
		memoryStore.Close()
		metricsStore.Close()
		plansStore.Close()
		if ownSubagents {
			_ = subagents.Close()
		}
		return Runtime{}, err
	}
	return Runtime{
		Agent: yourAgent, RunID: runID, LogPath: logPath, MemoryPath: memoryPath, MetricsPath: metricsPath, PlansPath: plansPath,
		SubagentsPath: subagentsPath, memory: memoryStore, metrics: metricsStore, plans: plansStore,
		subagents: subagents, ownSubagents: ownSubagents,
	}, nil
}

func NewSessionSummarizer(config Config) (session.Summarizer, error) {
	switch strings.ToLower(strings.TrimSpace(config.Provider)) {
	case "", "demo":
		return nil, nil
	case "openai":
		client := llmprovider.NewOpenAIClient(llmprovider.OpenAIConfig{
			BaseURL: config.BaseURL, APIKey: config.APIKey, UserAgent: "your-agent-session/0.2",
			HTTPClient: config.HTTPClient, MaxRetries: 2,
		})
		return model.NewSessionSummarizer(client, config.Model, config.FallbackModels...)
	default:
		return nil, fmt.Errorf("unsupported provider %q; use demo or openai", config.Provider)
	}
}

func (r Runtime) Close() error {
	var memoryErr, metricsErr, plansErr, subagentsErr error
	if r.memory != nil {
		memoryErr = r.memory.Close()
	}
	if r.metrics != nil {
		metricsErr = r.metrics.Close()
	}
	if r.plans != nil {
		plansErr = r.plans.Close()
	}
	if r.ownSubagents && r.subagents != nil {
		subagentsErr = r.subagents.Close()
	}
	return errors.Join(memoryErr, metricsErr, plansErr, subagentsErr)
}

func (r Runtime) MetricsSummary(ctx context.Context) (metricsstore.Summary, error) {
	if r.metrics == nil {
		return metricsstore.Summary{}, errors.New("metrics store is not initialized")
	}
	return r.metrics.Summary(ctx)
}

func newRunID() (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate run id: %w", err)
	}
	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05.000Z")
	return timestamp + "-" + hex.EncodeToString(random), nil
}
