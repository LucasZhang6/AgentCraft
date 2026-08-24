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

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/agent"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/evaluator"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/memory"
	metricsstore "github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/metrics"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/model"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/planning"
	llmprovider "github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/provider"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/session"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/tools"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/trajectory"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/knowledge"
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
}

type Runtime struct {
	Agent       *agent.PaperAgent
	RunID       string
	LogPath     string
	MemoryPath  string
	MetricsPath string
	PlansPath   string
	memory      *memory.Store
	metrics     *metricsstore.Store
	plans       *planning.Store
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
	registry := tools.NewRegistry(tools.RegistryOptions{
		DefaultTimeout: config.ToolTimeout,
		MaxOutputBytes: config.ToolOutputBytes,
		Approval:       config.Approval,
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
			BaseURL: config.BaseURL, APIKey: config.APIKey, UserAgent: "paper-agent/0.2",
			HTTPClient: config.HTTPClient, MaxRetries: 2,
		})
		openAIModel, err := model.NewOpenAIModel(model.OpenAIModelConfig{
			Client: client, Model: config.Model, FallbackModels: config.FallbackModels,
			MaxOutputTokens: config.MaxOutputTokens, PromptCacheKey: "paper-agent-runtime-v1", OnDelta: config.OnStream,
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
	var subAgent tools.SubAgentFunc
	if runner, ok := runtimeModel.(interface {
		RunSubAgent(context.Context, string) (string, error)
	}); ok {
		subAgent = runner.RunSubAgent
	}
	if err := tools.RegisterRuntimeTools(registry, tools.RuntimeOptions{
		WorkDir: config.WorkDir, HTTPClient: config.HTTPClient, Clarify: config.Clarify, SubAgent: subAgent,
	}); err != nil {
		memoryStore.Close()
		metricsStore.Close()
		plansStore.Close()
		return Runtime{}, fmt.Errorf("register runtime tools: %w", err)
	}
	if warnings, err := tools.RegisterExternalTools(context.Background(), registry, config.WorkDir); err != nil {
		memoryStore.Close()
		metricsStore.Close()
		return Runtime{}, fmt.Errorf("register external tools: %w", err)
	} else if config.OnEvent != nil {
		for _, warning := range warnings {
			config.OnEvent(domain.Event{Timestamp: time.Now().UTC(), Type: "plugin_warning", Payload: warning})
		}
	}

	paperAgent, err := agent.New(agent.Config{
		Model: runtimeModel, Tools: registry, Memory: memoryStore, Plans: planning.Validator{},
		PlanRecorder: planning.NewRecorder(plansStore, config.SessionID, runID),
		Evaluator:    evaluator.ReportEvaluator{}, Logger: trajectory.NewLogger(logPath, config.OnEvent),
		MetricsRecorder: metricsStore.Bind(runID),
		MaxSteps:        config.MaxSteps, MaxGoalTurns: config.MaxGoalTurns,
		TokenBudget: config.TokenBudget, MaxRecentObservations: config.MaxRecentObservations,
	})
	if err != nil {
		memoryStore.Close()
		metricsStore.Close()
		plansStore.Close()
		return Runtime{}, err
	}
	return Runtime{
		Agent: paperAgent, RunID: runID, LogPath: logPath, MemoryPath: memoryPath, MetricsPath: metricsPath, PlansPath: plansPath,
		memory: memoryStore, metrics: metricsStore, plans: plansStore,
	}, nil
}

func NewSessionSummarizer(config Config) (session.Summarizer, error) {
	switch strings.ToLower(strings.TrimSpace(config.Provider)) {
	case "", "demo":
		return nil, nil
	case "openai":
		client := llmprovider.NewOpenAIClient(llmprovider.OpenAIConfig{
			BaseURL: config.BaseURL, APIKey: config.APIKey, UserAgent: "paper-agent-session/0.2",
			HTTPClient: config.HTTPClient, MaxRetries: 2,
		})
		return model.NewSessionSummarizer(client, config.Model, config.FallbackModels...)
	default:
		return nil, fmt.Errorf("unsupported provider %q; use demo or openai", config.Provider)
	}
}

func (r Runtime) Close() error {
	var memoryErr, metricsErr, plansErr error
	if r.memory != nil {
		memoryErr = r.memory.Close()
	}
	if r.metrics != nil {
		metricsErr = r.metrics.Close()
	}
	if r.plans != nil {
		plansErr = r.plans.Close()
	}
	return errors.Join(memoryErr, metricsErr, plansErr)
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
