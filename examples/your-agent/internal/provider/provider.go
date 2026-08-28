package provider

import (
	"context"
	"net/http"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

type Request struct {
	Model           string
	FallbackModels  []string
	Instructions    string
	Input           string
	History         []domain.SessionMessage
	Images          []string
	Tools           []domain.ToolDescription
	MaxOutputTokens int
	PromptCacheKey  string
	Stream          bool
	OnDelta         func(string)
}

type Response struct {
	ID        string
	Text      string
	Blocks    []domain.ContentBlock
	Usage     domain.ModelUsage
	Truncated bool
}

type Client interface {
	Generate(context.Context, Request) (Response, error)
}

type StreamEvent struct {
	Delta    string
	Response *Response
	Err      error
}

type StreamingClient interface {
	Stream(context.Context, Request) (<-chan StreamEvent, error)
}

type OpenAIConfig struct {
	BaseURL        string
	APIKey         string
	UserAgent      string
	HTTPClient     *http.Client
	MaxRetries     int
	RetryBackoff   time.Duration
	RequestTimeout time.Duration
}
