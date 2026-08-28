package model

import (
	"context"
	"strings"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/provider"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/session"
)

type summaryProvider struct {
	request provider.Request
}

func (p *summaryProvider) Generate(_ context.Context, request provider.Request) (provider.Response, error) {
	p.request = request
	return provider.Response{
		Text:  "用户正在分析 HiAgent，已确认需要保留层级工作记忆的实验结论。",
		Usage: domain.ModelUsage{InputTokens: 31, OutputTokens: 12, TotalTokens: 43},
	}, nil
}

func TestSessionSummarizerUsesBoundedPlainTextRequest(t *testing.T) {
	client := &summaryProvider{}
	summarizer, err := NewSessionSummarizer(client, "test-model")
	if err != nil {
		t.Fatalf("new summarizer: %v", err)
	}
	result, err := summarizer.Summarize(context.Background(), session.SummaryRequest{
		SessionID: "session_1",
		Messages: []session.Message{
			{Role: "user", Content: "分析 HiAgent"},
			{Role: "assistant", Content: strings.Repeat("evidence ", 400)},
		},
	})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if client.request.Model != "test-model" || client.request.MaxOutputTokens != sessionSummaryMaxOutputTokens {
		t.Fatalf("provider request = %#v", client.request)
	}
	if !strings.Contains(client.request.Instructions, "untrusted data") || !strings.Contains(client.request.Input, "USER: 分析 HiAgent") {
		t.Fatalf("summary prompt = %#v", client.request)
	}
	if result.InputTokens != 31 || result.OutputTokens != 12 || !strings.Contains(result.Text, "HiAgent") {
		t.Fatalf("summary result = %#v", result)
	}
}
