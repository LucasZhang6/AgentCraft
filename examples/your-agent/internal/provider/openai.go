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

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
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
		if strings.TrimSpace(response.Text) == "" && !hasToolCalls(response.Blocks) {
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
				return c.recoverStream(ctx, request, events, attempt), nil
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

// recoverStream retries an interrupted SSE request only before any semantic
// event has escaped. parseSSE can turn complete output_item.done blocks into a
// truncated response; partial blocks still fail closed to avoid replaying work.
func (c *OpenAIClient) recoverStream(ctx context.Context, request Request, current <-chan StreamEvent, startedAttempt int) <-chan StreamEvent {
	output := make(chan StreamEvent, 32)
	go func() {
		defer close(output)
		attempt := startedAttempt
		for {
			emitted := false
			completed := false
			var streamErr error
			for event := range current {
				if event.Err != nil {
					streamErr = event.Err
					break
				}
				if event.Delta != "" || event.Response != nil {
					emitted = true
				}
				if event.Response != nil {
					completed = true
				}
				if !emitStream(ctx, output, event) {
					return
				}
			}
			if completed || streamErr == nil {
				return
			}
			if emitted || attempt >= c.maxRetries {
				emitStream(ctx, output, StreamEvent{Err: streamErr})
				return
			}

			for {
				if err := waitForRetry(ctx, retryDelay(c.retryBackoff, 0, attempt)); err != nil {
					emitStream(ctx, output, StreamEvent{Err: err})
					return
				}
				attempt++
				next, retryAfter, retryable, err := c.startStream(ctx, request)
				if err == nil {
					current = next
					break
				}
				streamErr = err
				if !retryable || attempt >= c.maxRetries {
					emitStream(ctx, output, StreamEvent{Err: streamErr})
					return
				}
				if retryAfter > 0 {
					if err := waitForRetry(ctx, retryAfter); err != nil {
						emitStream(ctx, output, StreamEvent{Err: err})
						return
					}
				}
			}
		}
	}()
	return output
}

func (c *OpenAIClient) requestBody(request Request, stream bool) ([]byte, error) {
	input, err := responsesInput(request)
	if err != nil {
		return nil, err
	}
	payload := responsesRequest{
		Model: request.Model, Instructions: request.Instructions,
		Input:           input,
		Tools:           responseTools(request.Tools),
		MaxOutputTokens: request.MaxOutputTokens, PromptCacheKey: strings.TrimSpace(request.PromptCacheKey),
		Store: false, Stream: stream,
	}
	if c.compatibleAPI {
		payload.MaxOutputTokens = 0
	} else {
		payload.Include = []string{"reasoning.encrypted_content"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("openai: encode request: %w", err)
	}
	return body, nil
}

func responsesInput(request Request) ([]any, error) {
	items := make([]any, 0, len(request.History)+1)
	for _, message := range request.History {
		converted, err := responsesHistoryItems(message)
		if err != nil {
			return nil, err
		}
		items = append(items, converted...)
	}
	var content []responsesInputContent
	if strings.TrimSpace(request.Input) != "" {
		content = append(content, responsesInputContent{Type: "input_text", Text: request.Input})
	}
	for _, image := range request.Images {
		content = append(content, responsesInputContent{Type: "input_image", ImageURL: image})
	}
	if len(content) > 0 {
		items = append(items, responsesInputMessage{Role: "user", Content: content})
	}
	return items, nil
}

func responsesHistoryItems(message domain.SessionMessage) ([]any, error) {
	role := strings.TrimSpace(message.Role)
	var items []any
	var textContent []responsesInputContent
	flushText := func() {
		if len(textContent) == 0 {
			return
		}
		messageRole := role
		if messageRole == "assistant_blocks" {
			messageRole = "assistant"
		}
		if messageRole != "assistant" {
			messageRole = "user"
		}
		items = append(items, responsesInputMessage{Role: messageRole, Content: textContent})
		textContent = nil
	}
	for _, block := range message.Blocks {
		switch block.Type {
		case domain.BlockText:
			textContent = append(textContent, responsesInputContent{Type: "input_text", Text: block.Text})
		case domain.BlockImage:
			textContent = append(textContent, responsesInputContent{Type: "input_image", ImageURL: block.ImageURL})
		case domain.BlockReasoning:
			flushText()
			if len(block.Raw) > 0 && json.Valid(block.Raw) {
				items = append(items, json.RawMessage(append([]byte(nil), block.Raw...)))
				continue
			}
			reasoning := map[string]any{"type": "reasoning"}
			if block.ReasoningID != "" {
				reasoning["id"] = block.ReasoningID
			}
			if block.EncryptedContent != "" {
				reasoning["encrypted_content"] = block.EncryptedContent
			}
			if block.ReasoningSummary != "" {
				reasoning["summary"] = []map[string]string{{"type": "summary_text", "text": block.ReasoningSummary}}
			}
			items = append(items, reasoning)
		case domain.BlockToolCall:
			flushText()
			arguments := strings.TrimSpace(string(block.Arguments))
			if arguments == "" {
				arguments = "{}"
			}
			if !json.Valid([]byte(arguments)) {
				return nil, fmt.Errorf("openai: invalid persisted arguments for tool call %q", block.ToolCallID)
			}
			items = append(items, map[string]any{
				"type": "function_call", "call_id": block.ToolCallID,
				"name": block.ToolName, "arguments": arguments,
			})
		case domain.BlockToolResult:
			flushText()
			items = append(items, map[string]any{
				"type": "function_call_output", "call_id": block.ToolCallID, "output": block.Output,
			})
		default:
			return nil, fmt.Errorf("openai: unsupported persisted content block %q", block.Type)
		}
	}
	flushText()
	return items, nil
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
	blocks := responseBlocks(payload)
	text := textFromBlocks(blocks)
	if strings.TrimSpace(text) == "" && !hasToolCalls(blocks) {
		return Response{}, 0, false, errors.New("openai: response contained no output text")
	}
	return responseFromPayload(payload), 0, false, nil
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
	var completedItems []json.RawMessage
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
		if envelope.Response.ID != "" {
			responseID = envelope.Response.ID
		}
		switch envelope.Type {
		case "response.output_text.delta", "response.refusal.delta":
			if envelope.Delta != "" {
				textValue.WriteString(envelope.Delta)
				return emitStream(ctx, events, StreamEvent{Delta: envelope.Delta})
			}
		case "response.output_item.done":
			if len(envelope.Item) > 0 && json.Valid(envelope.Item) {
				completedItems = append(completedItems, append(json.RawMessage(nil), envelope.Item...))
			}
		case "response.completed", "response.incomplete":
			terminated = true
			responseID = envelope.Response.ID
			finalUsage = usageFromPayload(envelope.Response)
			blocks := responseBlocks(envelope.Response)
			if envelope.Type == "response.incomplete" {
				blocks = responseBlocks(responsesResponse{Output: completedItems})
			} else if len(blocks) == 0 && len(completedItems) > 0 {
				blocks = responseBlocks(responsesResponse{Output: completedItems})
			}
			if textValue.Len() == 0 {
				fallback := textFromBlocks(blocks)
				if fallback != "" {
					textValue.WriteString(fallback)
					if !emitStream(ctx, events, StreamEvent{Delta: fallback}) {
						return false
					}
				}
			}
			if envelope.Type != "response.incomplete" && len(blocks) == 0 && textValue.Len() > 0 {
				blocks = []domain.ContentBlock{{Type: domain.BlockText, Text: textValue.String()}}
			}
			response := Response{
				ID: responseID, Text: textValue.String(), Blocks: blocks, Usage: finalUsage,
				Truncated: envelope.Type == "response.incomplete",
			}
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
		if emitSalvagedStreamResponse(ctx, events, responseID, completedItems, finalUsage) {
			return
		}
		emitStream(ctx, events, StreamEvent{Err: fmt.Errorf("openai: read SSE stream: %w", err)})
		return
	}
	if !terminated && ctx.Err() == nil {
		if emitSalvagedStreamResponse(ctx, events, responseID, completedItems, finalUsage) {
			return
		}
		emitStream(ctx, events, StreamEvent{Err: errors.New("openai: stream ended before response.completed")})
	}
}

func emitSalvagedStreamResponse(
	ctx context.Context,
	events chan<- StreamEvent,
	responseID string,
	completedItems []json.RawMessage,
	usage domain.ModelUsage,
) bool {
	if len(completedItems) == 0 {
		return false
	}
	blocks := responseBlocks(responsesResponse{Output: completedItems})
	if len(blocks) == 0 {
		return false
	}
	response := Response{
		ID: responseID, Text: textFromBlocks(blocks), Blocks: blocks, Usage: usage, Truncated: true,
	}
	return emitStream(ctx, events, StreamEvent{Response: &response})
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
	Model           string          `json:"model"`
	Instructions    string          `json:"instructions,omitempty"`
	Input           any             `json:"input"`
	Tools           []responsesTool `json:"tools,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	PromptCacheKey  string          `json:"prompt_cache_key,omitempty"`
	Include         []string        `json:"include,omitempty"`
	Store           bool            `json:"store"`
	Stream          bool            `json:"stream,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
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
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Output []json.RawMessage `json:"output"`
	Usage  responsesUsage    `json:"usage"`
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
	Item     json.RawMessage   `json:"item"`
	Response responsesResponse `json:"response"`
	Error    struct {
		Message string `json:"message"`
	} `json:"error"`
}

func responseBlocks(response responsesResponse) []domain.ContentBlock {
	var blocks []domain.ContentBlock
	for _, raw := range response.Output {
		var item struct {
			Type             string `json:"type"`
			ID               string `json:"id"`
			EncryptedContent string `json:"encrypted_content"`
			CallID           string `json:"call_id"`
			Name             string `json:"name"`
			Arguments        string `json:"arguments"`
			Summary          []struct {
				Text string `json:"text"`
			} `json:"summary"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		switch item.Type {
		case "reasoning":
			var summaries []string
			for _, summary := range item.Summary {
				if strings.TrimSpace(summary.Text) != "" {
					summaries = append(summaries, summary.Text)
				}
			}
			blocks = append(blocks, domain.ContentBlock{
				Type: domain.BlockReasoning, ReasoningID: item.ID,
				ReasoningSummary: strings.Join(summaries, "\n"), EncryptedContent: item.EncryptedContent,
				Raw: append(json.RawMessage(nil), raw...),
			})
		case "message":
			for _, content := range item.Content {
				if (content.Type == "output_text" || content.Type == "refusal") && strings.TrimSpace(content.Text) != "" {
					blocks = append(blocks, domain.ContentBlock{Type: domain.BlockText, Text: content.Text})
				}
			}
		case "function_call":
			arguments := json.RawMessage(item.Arguments)
			if !json.Valid(arguments) {
				arguments = json.RawMessage(`{}`)
			}
			blocks = append(blocks, domain.ContentBlock{
				Type: domain.BlockToolCall, ToolCallID: item.CallID, ToolName: item.Name, Arguments: arguments,
			})
		}
	}
	return blocks
}

func textFromBlocks(blocks []domain.ContentBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == domain.BlockText && strings.TrimSpace(block.Text) != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func hasToolCalls(blocks []domain.ContentBlock) bool {
	for _, block := range blocks {
		if block.Type == domain.BlockToolCall {
			return true
		}
	}
	return false
}

func responseTools(descriptions []domain.ToolDescription) []responsesTool {
	tools := make([]responsesTool, 0, len(descriptions))
	for _, description := range descriptions {
		parameters := append(json.RawMessage(nil), description.InputSchema...)
		if len(parameters) == 0 || !json.Valid(parameters) {
			encoded, err := json.Marshal(description.Schema)
			if err != nil {
				continue
			}
			parameters = encoded
		}
		tools = append(tools, responsesTool{
			Type: "function", Name: description.Name, Description: description.Description, Parameters: parameters,
		})
	}
	return tools
}

func responseFromPayload(payload responsesResponse) Response {
	blocks := responseBlocks(payload)
	return Response{
		ID: payload.ID, Text: textFromBlocks(blocks), Blocks: blocks, Usage: usageFromPayload(payload),
		Truncated: strings.EqualFold(strings.TrimSpace(payload.Status), "incomplete"),
	}
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
