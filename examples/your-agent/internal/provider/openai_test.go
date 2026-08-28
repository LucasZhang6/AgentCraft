package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/provider"
)

type failingReadCloser struct{ err error }

func (reader failingReadCloser) Read([]byte) (int, error) { return 0, reader.err }
func (failingReadCloser) Close() error                    { return nil }

type partialFailingReadCloser struct {
	data []byte
	err  error
}

func (reader *partialFailingReadCloser) Read(buffer []byte) (int, error) {
	if len(reader.data) > 0 {
		read := copy(buffer, reader.data)
		reader.data = reader.data[read:]
		return read, nil
	}
	return 0, reader.err
}

func (*partialFailingReadCloser) Close() error { return nil }

func TestOpenAIClientGeneratesTextAndUsage(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v1/responses" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "test-model" || payload["store"] != false {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		if _, exists := payload["max_output_tokens"]; exists {
			t.Fatalf("compatible request includes max_output_tokens: %#v", payload)
		}
		messages, ok := payload["input"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("input is not a one-message list: %#v", payload["input"])
		}
		content := messages[0].(map[string]any)["content"].([]any)
		if content[0].(map[string]any)["type"] != "input_text" || content[0].(map[string]any)["text"] != "hello" {
			t.Fatalf("input content = %#v", content)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "id":"resp_123",
  "output":[{"type":"message","content":[{"type":"output_text","text":"{\"ok\":true}"}]}],
  "usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}
}`)),
		}, nil
	})}

	client := provider.NewOpenAIClient(provider.OpenAIConfig{
		BaseURL: "https://example.test/v1", APIKey: "test-key", HTTPClient: httpClient,
	})
	response, err := client.Generate(context.Background(), provider.Request{
		Model: "test-model", Instructions: "return json", Input: "hello",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if response.Text != `{"ok":true}` {
		t.Fatalf("text = %q", response.Text)
	}
	if response.Usage.TotalTokens != 18 || response.Usage.InputTokens != 11 || response.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %#v", response.Usage)
	}
}

func TestOpenAIStreamingFallbackPromptCacheAndUsage(t *testing.T) {
	var mu sync.Mutex
	var models []string
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		model := payload["model"].(string)
		mu.Lock()
		models = append(models, model)
		mu.Unlock()
		if payload["stream"] != true || payload["prompt_cache_key"] != "your-agent-test" {
			t.Fatalf("stream/cache payload = %#v", payload)
		}
		if model == "primary" {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"overloaded"}`)),
			}, nil
		}
		body := strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"你"}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"好"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_stream","usage":{"input_tokens":9,"output_tokens":2,"total_tokens":11,"input_tokens_details":{"cached_tokens":6}}}}`,
			"",
		}, "\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	client := provider.NewOpenAIClient(provider.OpenAIConfig{
		BaseURL: "https://example.test/v1", APIKey: "test-key", HTTPClient: httpClient, MaxRetries: 0,
	})
	var deltas strings.Builder
	response, err := client.Generate(context.Background(), provider.Request{
		Model: "primary", FallbackModels: []string{"fallback"}, Input: "hello", Stream: true,
		PromptCacheKey: "your-agent-test", OnDelta: func(delta string) { deltas.WriteString(delta) },
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if response.Text != "你好" || deltas.String() != "你好" {
		t.Fatalf("response=%q deltas=%q", response.Text, deltas.String())
	}
	if response.Usage.TotalTokens != 11 || response.Usage.CacheReadInputTokens != 6 {
		t.Fatalf("usage = %#v", response.Usage)
	}
	if strings.Join(models, ",") != "primary,fallback" {
		t.Fatalf("models = %#v", models)
	}
}

func TestOpenAIStreamingRetriesInterruptionBeforeOutput(t *testing.T) {
	var requests int
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: failingReadCloser{err: io.ErrUnexpectedEOF},
			}, nil
		}
		body := strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"recovered"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_recovered","usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}}`,
			"",
		}, "\n")
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	client := provider.NewOpenAIClient(provider.OpenAIConfig{
		BaseURL: "https://example.test/v1", APIKey: "test-key", HTTPClient: httpClient,
		MaxRetries: 1, RetryBackoff: time.Millisecond,
	})
	response, err := client.Generate(context.Background(), provider.Request{Model: "test-model", Input: "hello", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "recovered" || requests != 2 {
		t.Fatalf("response=%#v requests=%d", response, requests)
	}
}

func TestOpenAIStreamingDoesNotReplayAfterPartialOutput(t *testing.T) {
	var requests int
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: &partialFailingReadCloser{
				data: []byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"),
				err:  io.ErrUnexpectedEOF,
			},
		}, nil
	})}
	client := provider.NewOpenAIClient(provider.OpenAIConfig{
		BaseURL: "https://example.test/v1", APIKey: "test-key", HTTPClient: httpClient,
		MaxRetries: 2, RetryBackoff: time.Millisecond,
	})
	var deltas strings.Builder
	_, err := client.Generate(context.Background(), provider.Request{
		Model: "test-model", Input: "hello", Stream: true,
		OnDelta: func(delta string) { deltas.WriteString(delta) },
	})
	if err == nil || !strings.Contains(err.Error(), "read SSE stream") {
		t.Fatalf("error = %v", err)
	}
	if requests != 1 || deltas.String() != "partial" {
		t.Fatalf("requests=%d deltas=%q", requests, deltas.String())
	}
}

func TestOpenAIStreamingSalvagesCompletedToolCallAfterInterruption(t *testing.T) {
	var requests int
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: &partialFailingReadCloser{
				data: []byte("data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call_safe\",\"name\":\"file_read\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}\n\n"),
				err:  io.ErrUnexpectedEOF,
			},
		}, nil
	})}
	client := provider.NewOpenAIClient(provider.OpenAIConfig{
		BaseURL: "https://example.test/v1", APIKey: "test-key", HTTPClient: httpClient,
		MaxRetries: 2, RetryBackoff: time.Millisecond,
	})
	response, err := client.Generate(context.Background(), provider.Request{
		Model: "test-model", Input: "read the file", Stream: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || !response.Truncated || len(response.Blocks) != 1 {
		t.Fatalf("response=%#v requests=%d", response, requests)
	}
	call := response.Blocks[0]
	if call.Type != domain.BlockToolCall || call.ToolCallID != "call_safe" || call.ToolName != "file_read" || string(call.Arguments) != `{"path":"README.md"}` {
		t.Fatalf("salvaged call = %#v", call)
	}
}

func TestOpenAIIncompleteStreamUsesOnlyCompletedMessageBlocks(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"message","content":[{"type":"output_text","text":"complete sentence"}]}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"unfinished tail"}`,
		"",
		`data: {"type":"response.incomplete","response":{"id":"resp_incomplete","status":"incomplete"}}`,
		"",
	}, "\n")
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	client := provider.NewOpenAIClient(provider.OpenAIConfig{
		BaseURL: "https://example.test/v1", APIKey: "test-key", HTTPClient: httpClient,
	})
	response, err := client.Generate(context.Background(), provider.Request{Model: "test-model", Input: "continue", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Truncated || len(response.Blocks) != 1 || response.Blocks[0].Text != "complete sentence" {
		t.Fatalf("response = %#v", response)
	}
}

func TestOpenAIIncompleteStreamDoesNotPromotePartialDeltaToBlock(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"unfinished tail"}`,
		"",
		`data: {"type":"response.incomplete","response":{"id":"resp_partial","status":"incomplete"}}`,
		"",
	}, "\n")
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(body)),
		}, nil
	})}
	client := provider.NewOpenAIClient(provider.OpenAIConfig{
		BaseURL: "https://example.test/v1", APIKey: "test-key", HTTPClient: httpClient,
	})
	response, err := client.Generate(context.Background(), provider.Request{Model: "test-model", Input: "continue", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Truncated || len(response.Blocks) != 0 || response.Text != "unfinished tail" {
		t.Fatalf("response = %#v", response)
	}
}

func TestOfficialOpenAIRequestKeepsOutputLimitAndImages(t *testing.T) {
	const image = "data:image/png;base64,iVBORw0KGgo="
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["max_output_tokens"] != float64(777) {
			t.Fatalf("max_output_tokens = %#v", payload["max_output_tokens"])
		}
		include, ok := payload["include"].([]any)
		if !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
			t.Fatalf("reasoning include = %#v", payload["include"])
		}
		messages, ok := payload["input"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("input = %#v", payload["input"])
		}
		message := messages[0].(map[string]any)
		content := message["content"].([]any)
		if len(content) != 2 || content[1].(map[string]any)["image_url"] != image {
			t.Fatalf("content = %#v", content)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "id":"resp_image",
  "output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]
}`)),
		}, nil
	})}
	client := provider.NewOpenAIClient(provider.OpenAIConfig{APIKey: "test-key", HTTPClient: httpClient})
	response, err := client.Generate(context.Background(), provider.Request{
		Model: "test-model", Input: "describe", Images: []string{image}, MaxOutputTokens: 777,
	})
	if err != nil || response.Text != "ok" {
		t.Fatalf("response = %#v, err = %v", response, err)
	}
}

func TestOpenAIRequestReplaysStructuredSessionItems(t *testing.T) {
	reasoningRaw := json.RawMessage(`{"type":"reasoning","id":"rs_1","encrypted_content":"cipher","summary":[{"type":"summary_text","text":"checked evidence"}]}`)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		items := payload["input"].([]any)
		if len(items) != 6 {
			t.Fatalf("input items = %d, want 6: %#v", len(items), items)
		}
		if user := items[0].(map[string]any); user["role"] != "user" {
			t.Fatalf("restored user item = %#v", user)
		}
		if reasoning := items[1].(map[string]any); reasoning["type"] != "reasoning" || reasoning["id"] != "rs_1" || reasoning["encrypted_content"] != "cipher" {
			t.Fatalf("restored reasoning item = %#v", reasoning)
		}
		if call := items[2].(map[string]any); call["type"] != "function_call" || call["call_id"] != "call_42" || call["name"] != "web_search" {
			t.Fatalf("restored function call = %#v", call)
		}
		if result := items[3].(map[string]any); result["type"] != "function_call_output" || result["call_id"] != "call_42" {
			t.Fatalf("restored function output = %#v", result)
		}
		if assistant := items[4].(map[string]any); assistant["role"] != "assistant" {
			t.Fatalf("restored assistant item = %#v", assistant)
		}
		if current := items[5].(map[string]any); current["role"] != "user" {
			t.Fatalf("current input item = %#v", current)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "id":"resp_structured",
  "output":[
    {"type":"reasoning","id":"rs_2","encrypted_content":"next-cipher","summary":[{"type":"summary_text","text":"next reasoning"}]},
    {"type":"message","content":[{"type":"output_text","text":"done"}]}
  ],
  "usage":{"input_tokens":20,"output_tokens":3,"total_tokens":23}
}`)),
		}, nil
	})}
	client := provider.NewOpenAIClient(provider.OpenAIConfig{
		BaseURL: "https://example.test/v1", APIKey: "test-key", HTTPClient: httpClient,
	})
	response, err := client.Generate(context.Background(), provider.Request{
		Model: "test-model", Input: "continue",
		History: []domain.SessionMessage{
			{Role: "user", Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: "find evidence"}}},
			{Role: "assistant_blocks", Blocks: []domain.ContentBlock{
				{Type: domain.BlockReasoning, ReasoningID: "rs_1", EncryptedContent: "cipher", Raw: reasoningRaw},
				{Type: domain.BlockToolCall, ToolCallID: "call_42", ToolName: "web_search", Arguments: json.RawMessage(`{"query":"agent memory"}`)},
			}},
			{Role: "tool_results", Blocks: []domain.ContentBlock{{Type: domain.BlockToolResult, ToolCallID: "call_42", Output: `{"title":"A-MEM"}`}}},
			{Role: "assistant", Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: "partial conclusion"}}},
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if response.Text != "done" || len(response.Blocks) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if response.Blocks[0].Type != domain.BlockReasoning || response.Blocks[0].ReasoningID != "rs_2" || string(response.Blocks[0].Raw) == "" {
		t.Fatalf("reasoning block = %#v", response.Blocks[0])
	}
}

func TestOpenAIRequestSendsNativeToolsAndAcceptsToolOnlyResponse(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		tools, ok := payload["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("native tools = %#v", payload["tools"])
		}
		tool := tools[0].(map[string]any)
		parameters := tool["parameters"].(map[string]any)
		if tool["type"] != "function" || tool["name"] != "search_papers" || parameters["type"] != "object" {
			t.Fatalf("native tool payload = %#v", tool)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
  "id":"resp_tool",
  "output":[{"type":"function_call","call_id":"call_1","name":"search_papers","arguments":"{\"query\":\"memory\"}"}],
  "usage":{"input_tokens":8,"output_tokens":2,"total_tokens":10}
}`)),
		}, nil
	})}
	client := provider.NewOpenAIClient(provider.OpenAIConfig{
		BaseURL: "https://example.test/v1", APIKey: "test-key", HTTPClient: httpClient,
	})
	response, err := client.Generate(context.Background(), provider.Request{
		Model: "test-model", Input: "search", Tools: []domain.ToolDescription{{
			Name: "search_papers", Description: "Search papers",
			Schema: domain.ToolSchema{Type: "object", Required: []string{"query"}, Properties: map[string]domain.ToolField{"query": {Type: "string"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text != "" || len(response.Blocks) != 1 || response.Blocks[0].Type != domain.BlockToolCall || response.Blocks[0].ToolCallID != "call_1" {
		t.Fatalf("tool-only response = %#v", response)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
