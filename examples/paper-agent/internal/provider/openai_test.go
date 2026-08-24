package provider_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/provider"
)

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
		if payload["stream"] != true || payload["prompt_cache_key"] != "paper-agent-test" {
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
		PromptCacheKey: "paper-agent-test", OnDelta: func(delta string) { deltas.WriteString(delta) },
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
