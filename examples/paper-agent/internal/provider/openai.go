package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
)

const (
	defaultOpenAIBaseURL         = "https://api.openai.com/v1"
	defaultMaxOutputTokens       = 4096
	defaultRequestTimeout        = 2 * time.Minute
	defaultRetryBackoff          = 500 * time.Millisecond
	maxErrorResponseBody   int64 = 16 * 1024
)

type OpenAIClient struct {
	baseURL        string
	apiKey         string
	userAgent      string
	compatibleAPI  bool
	httpClient     *http.Client
	maxRetries     int
	retryBackoff   time.Duration
	requestTimeout time.Duration
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openai: HTTP %d: %s", e.StatusCode, e.Body)
}

func NewOpenAIClient(config OpenAIConfig) *OpenAIClient {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	compatibleAPI := baseURL != "" && baseURL != defaultOpenAIBaseURL
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	backoff := config.RetryBackoff
	if backoff <= 0 {
		backoff = defaultRetryBackoff
	}
	timeout := config.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return &OpenAIClient{
		baseURL:        baseURL,
		apiKey:         strings.TrimSpace(config.APIKey),
		userAgent:      strings.TrimSpace(config.UserAgent),
		compatibleAPI:  compatibleAPI,
		httpClient:     client,
		maxRetries:     max(config.MaxRetries, 0),
		retryBackoff:   backoff,
		requestTimeout: timeout,
	}
}

func (c *OpenAIClient) Generate(ctx context.Context, request Request) (Response, error) {
	if request.Stream {
		events, err := c.Stream(ctx, request)
		if err != nil {
			return Response{}, err
		}
		var response Response
		var text strings.Builder
		for event := range events {
			if event.Err != nil {
				return Response{}, event.Err
			}
			if event.Delta != "" {
				text.WriteString(event.Delta)
				if request.OnDelta != nil {
					request.OnDelta(event.Delta)
				}
			}
			if event.Response != nil {
				response = *event.Response
			}
		}
		if response.Text == "" {
			response.Text = text.String()
		}
		if strings.TrimSpace(response.Text) == "" {
			return Response{}, errors.New("openai: stream contained no output text")
		}
		return response, nil
	}
	return c.generateWithFallback(ctx, request)
}

func (c *OpenAIClient) generateWithFallback(ctx context.Context, request Request) (Response, error) {
	if c == nil {
		return Response{}, errors.New("openai: client is nil")
	}
	if c.apiKey == "" {
		return Response{}, errors.New("openai: missing API key")
	}
	if strings.TrimSpace(request.Model) == "" {
		return Response{}, errors.New("openai: model is required")
	}
	if request.MaxOutputTokens <= 0 {
		request.MaxOutputTokens = defaultMaxOutputTokens
	}
	models := fallbackChain(request.Model, request.FallbackModels)
	var lastErr error
	for modelIndex, modelID := range models {
		request.Model = modelID
		body, err := c.requestBody(request, false)
		if err != nil {
			return Response{}, err
		}
		for attempt := 0; attempt <= c.maxRetries; attempt++ {
			response, retryAfter, retryable, err := c.do(ctx, body)
			if err == nil {
				return response, nil
			}
			lastErr = err
			if !retryable || attempt == c.maxRetries {
				break
			}
			if err := waitForRetry(ctx, retryDelay(c.retryBackoff, retryAfter, attempt)); err != nil {
				return Response{}, err
			}
		}
		if !fallbackEligible(lastErr) || modelIndex == len(models)-1 {
			return Response{}, lastErr
		}
	}
	return Response{}, fmt.Errorf("openai: model fallback exhausted: %w", lastErr)
}

func (c *OpenAIClient) Stream(ctx context.Context, request Request) (<-chan StreamEvent, error) {
	if c == nil {
		return nil, errors.New("openai: client is nil")
	}
	if c.apiKey == "" {
		return nil, errors.New("openai: missing API key")
	}
	if strings.TrimSpace(request.Model) == "" {
		return nil, errors.New("openai: model is required")
	}
	if request.MaxOutputTokens <= 0 {
		request.MaxOutputTokens = defaultMaxOutputTokens
	}
	models := fallbackChain(request.Model, request.FallbackModels)
	var lastErr error
	for modelIndex, modelID := range models {
		request.Model = modelID
		for attempt := 0; attempt <= c.maxRetries; attempt++ {
			events, retryAfter, retryable, err := c.startStream(ctx, request)
			if err == nil {
				return events, nil
			}
			lastErr = err
			if !retryable || attempt == c.maxRetries {
				break
			}
			if err := waitForRetry(ctx, retryDelay(c.retryBackoff, retryAfter, attempt)); err != nil {
				return nil, err
			}
		}
		if !fallbackEligible(lastErr) || modelIndex == len(models)-1 {
			return nil, lastErr
		}
	}
	return nil, fmt.Errorf("openai: streaming model fallback exhausted: %w", lastErr)
}

func (c *OpenAIClient) requestBody(request Request, stream bool) ([]byte, error) {
	content := []responsesInputContent{{Type: "input_text", Text: request.Input}}
	for _, image := range request.Images {
		content = append(content, responsesInputContent{Type: "input_image", ImageURL: image})
	}
	payload := responsesRequest{
		Model: request.Model, Instructions: request.Instructions,
		Input:           []responsesInputMessage{{Role: "user", Content: content}},
		MaxOutputTokens: request.MaxOutputTokens, PromptCacheKey: strings.TrimSpace(request.PromptCacheKey),
		Store: false, Stream: stream,
	}
	if c.compatibleAPI {
		payload.MaxOutputTokens = 0
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai: encode request: %w", err)
	}
	return body, nil
}

func (c *OpenAIClient) do(ctx context.Context, body []byte) (Response, time.Duration, bool, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return Response{}, 0, false, fmt.Errorf("openai: build request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		httpRequest.Header.Set("User-Agent", c.userAgent)
	}

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if requestCtx.Err() != nil {
			return Response{}, 0, false, requestCtx.Err()
		}
		return Response{}, 0, true, fmt.Errorf("openai: request failed: %w", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(httpResponse.Body, maxErrorResponseBody))
		retryable := httpResponse.StatusCode == http.StatusTooManyRequests || httpResponse.StatusCode >= 500
		return Response{}, parseRetryAfter(httpResponse.Header.Get("Retry-After")), retryable,
			&APIError{StatusCode: httpResponse.StatusCode, Body: strings.TrimSpace(string(data))}
	}

	var payload responsesResponse
	if err := json.NewDecoder(io.LimitReader(httpResponse.Body, 8*1024*1024)).Decode(&payload); err != nil {
		return Response{}, 0, false, fmt.Errorf("openai: decode response: %w", err)
	}
	text := responseText(payload)
	if strings.TrimSpace(text) == "" {
		return Response{}, 0, false, errors.New("openai: response contained no output text")
	}
	usage := domain.ModelUsage{
		InputTokens:          payload.Usage.InputTokens,
		OutputTokens:         payload.Usage.OutputTokens,
		TotalTokens:          payload.Usage.TotalTokens,
		CacheReadInputTokens: payload.Usage.InputTokenDetails.CachedTokens,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return Response{ID: payload.ID, Text: text, Usage: usage}, 0, false, nil
}

func (c *OpenAIClient) startStream(ctx context.Context, request Request) (<-chan StreamEvent, time.Duration, bool, error) {
	body, err := c.requestBody(request, true)
	if err != nil {
		return nil, 0, false, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, 0, false, fmt.Errorf("openai: build streaming request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream, application/json")
	if c.userAgent != "" {
		httpRequest.Header.Set("User-Agent", c.userAgent)
	}
	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		cancel()
		if requestCtx.Err() != nil {
			return nil, 0, false, requestCtx.Err()
		}
		return nil, 0, true, fmt.Errorf("openai: streaming request failed: %w", err)
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		defer httpResponse.Body.Close()
		defer cancel()
		data, _ := io.ReadAll(io.LimitReader(httpResponse.Body, maxErrorResponseBody))
		retryable := httpResponse.StatusCode == http.StatusTooManyRequests || httpResponse.StatusCode >= 500
		return nil, parseRetryAfter(httpResponse.Header.Get("Retry-After")), retryable,
			&APIError{StatusCode: httpResponse.StatusCode, Body: strings.TrimSpace(string(data))}
	}

	events := make(chan StreamEvent, 32)
	go func() {
		defer close(events)
		defer httpResponse.Body.Close()
		defer cancel()
		contentType := strings.ToLower(httpResponse.Header.Get("Content-Type"))
		if strings.Contains(contentType, "application/json") {
			var payload responsesResponse
			if err := json.NewDecoder(io.LimitReader(httpResponse.Body, 8*1024*1024)).Decode(&payload); err != nil {
				emitStream(ctx, events, StreamEvent{Err: fmt.Errorf("openai: decode streaming fallback: %w", err)})
				return
			}
			response := responseFromPayload(payload)
			if response.Text != "" {
				emitStream(ctx, events, StreamEvent{Delta: response.Text})
			}
			emitStream(ctx, events, StreamEvent{Response: &response})
			return
		}
		parseSSE(ctx, httpResponse.Body, events)
	}()
	return events, 0, false, nil
}

func parseSSE(ctx context.Context, reader io.Reader, events chan<- StreamEvent) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var data strings.Builder
	var textValue strings.Builder
	var responseID string
	var finalUsage domain.ModelUsage
	terminated := false
	flush := func() bool {
		if data.Len() == 0 {
			return true
		}
		raw := data.String()
		data.Reset()
		if strings.TrimSpace(raw) == "[DONE]" {
			return true
		}
		var envelope responsesStreamEnvelope
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			return emitStream(ctx, events, StreamEvent{Err: fmt.Errorf("openai: decode SSE event: %w", err)})
		}
		switch envelope.Type {
		case "response.output_text.delta", "response.refusal.delta":
			if envelope.Delta != "" {
				textValue.WriteString(envelope.Delta)
				return emitStream(ctx, events, StreamEvent{Delta: envelope.Delta})
			}
		case "response.completed", "response.incomplete":
			terminated = true
			responseID = envelope.Response.ID
			finalUsage = usageFromPayload(envelope.Response)
			if textValue.Len() == 0 {
				fallback := responseText(envelope.Response)
				if fallback != "" {
					textValue.WriteString(fallback)
					if !emitStream(ctx, events, StreamEvent{Delta: fallback}) {
						return false
					}
				}
			}
			response := Response{ID: responseID, Text: textValue.String(), Usage: finalUsage}
			return emitStream(ctx, events, StreamEvent{Response: &response})
		case "response.failed", "error":
			terminated = true
			message := strings.TrimSpace(envelope.Error.Message)
			if message == "" {
				message = "stream failed"
			}
			return emitStream(ctx, events, StreamEvent{Err: errors.New("openai: " + message)})
		}
		return true
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !flush() {
				return
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	if !flush() {
		return
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		emitStream(ctx, events, StreamEvent{Err: fmt.Errorf("openai: read SSE stream: %w", err)})
		return
	}
	if !terminated && ctx.Err() == nil {
		emitStream(ctx, events, StreamEvent{Err: errors.New("openai: stream ended before response.completed")})
	}
}

func emitStream(ctx context.Context, events chan<- StreamEvent, event StreamEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case events <- event:
		return true
	}
}

func fallbackChain(primary string, fallbacks []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(fallbacks)+1)
	for _, model := range append([]string{primary}, fallbacks...) {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	return result
}

func fallbackEligible(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500)
}

func retryDelay(base, retryAfter time.Duration, attempt int) time.Duration {
	wait := base * time.Duration(1<<attempt)
	if retryAfter > wait {
		wait = retryAfter
	}
	if wait > 15*time.Second {
		wait = 15 * time.Second
	}
	return wait
}

func waitForRetry(ctx context.Context, wait time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}

type responsesRequest struct {
	Model           string `json:"model"`
	Instructions    string `json:"instructions,omitempty"`
	Input           any    `json:"input"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
	PromptCacheKey  string `json:"prompt_cache_key,omitempty"`
	Store           bool   `json:"store"`
	Stream          bool   `json:"stream,omitempty"`
}

type responsesInputMessage struct {
	Role    string                  `json:"role"`
	Content []responsesInputContent `json:"content"`
}

type responsesInputContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type responsesResponse struct {
	ID     string `json:"id"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage responsesUsage `json:"usage"`
}

type responsesUsage struct {
	InputTokens       int `json:"input_tokens"`
	OutputTokens      int `json:"output_tokens"`
	TotalTokens       int `json:"total_tokens"`
	InputTokenDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

type responsesStreamEnvelope struct {
	Type     string            `json:"type"`
	Delta    string            `json:"delta"`
	Response responsesResponse `json:"response"`
	Error    struct {
		Message string `json:"message"`
	} `json:"error"`
}

func responseText(response responsesResponse) string {
	var parts []string
	for _, item := range response.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func responseFromPayload(payload responsesResponse) Response {
	return Response{ID: payload.ID, Text: responseText(payload), Usage: usageFromPayload(payload)}
}

func usageFromPayload(payload responsesResponse) domain.ModelUsage {
	usage := domain.ModelUsage{
		InputTokens: payload.Usage.InputTokens, OutputTokens: payload.Usage.OutputTokens,
		TotalTokens: payload.Usage.TotalTokens, CacheReadInputTokens: payload.Usage.InputTokenDetails.CachedTokens,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage
}

func parseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
