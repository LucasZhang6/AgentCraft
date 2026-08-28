package domain

import (
	"encoding/json"
	"time"
)

const (
	PlanPending   = "pending"
	PlanRunning   = "running"
	PlanCompleted = "completed"
	PlanFailed    = "failed"
	PlanWaiting   = "awaiting_acceptance"
	PlanSkipped   = "skipped"

	RiskRead      = "read"
	RiskWrite     = "write"
	RiskDangerous = "dangerous"

	MemoryActive   = "active"
	MemoryArchived = "archived"

	GoalPursuing = "pursuing"
	GoalAchieved = "achieved"
	GoalStopped  = "stopped"

	BlockText       = "text"
	BlockImage      = "image"
	BlockReasoning  = "reasoning"
	BlockToolCall   = "tool_call"
	BlockToolResult = "tool_result"
)

// ContentBlock is the provider-neutral durable form of one native conversation
// item. Raw preserves provider-owned reasoning state that must be replayed
// byte-for-byte, while the normalized fields keep the store inspectable.
type ContentBlock struct {
	Type             string          `json:"type"`
	Text             string          `json:"text,omitempty"`
	ImageURL         string          `json:"imageUrl,omitempty"`
	ReasoningID      string          `json:"reasoningId,omitempty"`
	ReasoningSummary string          `json:"reasoningSummary,omitempty"`
	EncryptedContent string          `json:"encryptedContent,omitempty"`
	ToolCallID       string          `json:"toolCallId,omitempty"`
	ToolName         string          `json:"toolName,omitempty"`
	Arguments        json.RawMessage `json:"arguments,omitempty"`
	Output           string          `json:"output,omitempty"`
	IsError          bool            `json:"isError,omitempty"`
	Raw              json.RawMessage `json:"raw,omitempty"`
}

type SessionMessage struct {
	Role   string         `json:"role"`
	Blocks []ContentBlock `json:"blocks"`
}

type AcceptanceCheck struct {
	Type     string `json:"type"`
	Path     string `json:"path,omitempty"`
	Expected string `json:"expected,omitempty"`
}

type PlanStep struct {
	ID               string            `json:"id"`
	Description      string            `json:"description"`
	Dependencies     []string          `json:"dependencies"`
	Tool             string            `json:"tool,omitempty"` // Legacy preferred tool; normalized into AllowedTools.
	AllowedTools     []string          `json:"allowedTools,omitempty"`
	SuccessCriteria  string            `json:"successCriteria"`
	Status           string            `json:"status"`
	AgentRole        string            `json:"agentRole,omitempty"`
	Acceptance       []AcceptanceCheck `json:"acceptance,omitempty"`
	Evidence         []string          `json:"evidence,omitempty"`
	Output           string            `json:"output,omitempty"`
	Attempts         int               `json:"attempts,omitempty"`
	RequiresApproval bool              `json:"requiresApproval,omitempty"`
}

type ToolField struct {
	Type string `json:"type"`
}

type ToolSchema struct {
	Type       string               `json:"type"`
	Required   []string             `json:"required,omitempty"`
	Properties map[string]ToolField `json:"properties"`
}

type ToolDescription struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Schema           ToolSchema      `json:"schema"`
	InputSchema      json.RawMessage `json:"inputSchema,omitempty"`
	Risk             string          `json:"risk"`
	SupportsParallel bool            `json:"supportsParallel,omitempty"`
	ConcurrencyGroup string          `json:"concurrencyGroup,omitempty"`
}

type ToolExecution struct {
	Result            any   `json:"result,omitempty"`
	DurationMS        int64 `json:"durationMs"`
	OutputBytes       int   `json:"outputBytes"`
	Truncated         bool  `json:"truncated"`
	ApprovalRequested bool  `json:"approvalRequested"`
}

type StepContext struct {
	Goal           string            `json:"goal"`
	PlanID         string            `json:"planId"`
	GoalTurn       int               `json:"goalTurn"`
	Images         []string          `json:"-"`
	Step           PlanStep          `json:"step"`
	Plan           []PlanStep        `json:"plan"`
	ContextSummary string            `json:"contextSummary,omitempty"`
	Observations   []Observation     `json:"recentObservations"`
	Memories       []Memory          `json:"memories"`
	Skills         string            `json:"skills,omitempty"`
	Tools          []ToolDescription `json:"-"`
	SessionHistory []SessionMessage  `json:"-"`
}

type ModelTurn struct {
	Blocks    []ContentBlock
	Usage     ModelUsage
	Truncated bool
}

type FinalContext struct {
	Goal           string           `json:"goal"`
	PlanID         string           `json:"planId"`
	Plan           []PlanStep       `json:"plan"`
	ContextSummary string           `json:"contextSummary,omitempty"`
	Observations   []Observation    `json:"observations"`
	Memories       []Memory         `json:"memories"`
	Images         []string         `json:"-"`
	SessionHistory []SessionMessage `json:"-"`
}

type FinalResponse struct {
	Content string
	Blocks  []ContentBlock
	Usage   ModelUsage
}

type Observation struct {
	ToolCallID string         `json:"toolCallId,omitempty"`
	Tool       string         `json:"tool"`
	Args       map[string]any `json:"args,omitempty"`
	Result     any            `json:"result,omitempty"`
	Error      string         `json:"error,omitempty"`
	OK         bool           `json:"ok"`
}

type Evaluation struct {
	Passed bool            `json:"passed"`
	Score  float64         `json:"score"`
	Checks map[string]bool `json:"checks"`
}

type Memory struct {
	ID         string  `json:"id"`
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence"`
	Scope      string  `json:"scope"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"createdAt"`
	UpdatedAt  string  `json:"updatedAt"`
	LastUsedAt string  `json:"lastUsedAt,omitempty"`
}

type MemoryInput struct {
	Key        string
	Value      string
	Source     string
	Confidence float64
	Scope      string
}

type MemoryQuery struct {
	Text       string
	Scopes     []string
	Limit      int
	LimitBytes int
}

type ModelUsage struct {
	InputTokens              int `json:"inputTokens"`
	OutputTokens             int `json:"outputTokens"`
	TotalTokens              int `json:"totalTokens"`
	CacheReadInputTokens     int `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int `json:"cacheCreationInputTokens"`
}

type PlanResponse struct {
	Plan            []PlanStep
	Usage           ModelUsage
	ReasoningBlocks []ContentBlock
}

type CompactionContext struct {
	Goal            string        `json:"goal"`
	PreviousSummary string        `json:"previousSummary,omitempty"`
	Observations    []Observation `json:"observations"`
}

type CompactionResponse struct {
	Summary string
	Usage   ModelUsage
}

type GoalState struct {
	Objective   string `json:"objective"`
	Status      string `json:"status"`
	Turns       int    `json:"turns"`
	MaxTurns    int    `json:"maxTurns"`
	TokenBudget int    `json:"tokenBudget"`
	TokensUsed  int    `json:"tokensUsed"`
	StopReason  string `json:"stopReason,omitempty"`
}

type RunMetrics struct {
	StartedAt                time.Time `json:"startedAt"`
	CompletedAt              time.Time `json:"completedAt"`
	DurationMS               int64     `json:"durationMs"`
	LLMCalls                 int       `json:"llmCalls"`
	InputTokens              int       `json:"inputTokens"`
	OutputTokens             int       `json:"outputTokens"`
	TotalTokens              int       `json:"totalTokens"`
	CacheReadInputTokens     int       `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int       `json:"cacheCreationInputTokens"`
	ToolCalls                int       `json:"toolCalls"`
	ToolFailures             int       `json:"toolFailures"`
	ToolDurationMS           int64     `json:"toolDurationMs"`
	HumanApprovalRequests    int       `json:"humanApprovalRequests"`
	ContextCompactions       int       `json:"contextCompactions"`
	GoalTurns                int       `json:"goalTurns"`
	Success                  bool      `json:"success"`
}

type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Payload   any       `json:"payload"`
}

type Paper struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Module       string   `json:"module"`
	Keywords     []string `json:"keywords"`
	URL          string   `json:"url"`
	Problem      string   `json:"problem"`
	Method       string   `json:"method"`
	Contribution string   `json:"contribution"`
	Limitation   string   `json:"limitation"`
	Engineering  string   `json:"engineering"`
}

type PaperMatch struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Module string `json:"module"`
	Score  int    `json:"score"`
}
