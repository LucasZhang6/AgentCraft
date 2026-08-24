package model

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/provider"
	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/session"
)

func TestLiveSessionSummarizer(t *testing.T) {
	if os.Getenv("PAPER_AGENT_LIVE_TEST") != "1" {
		t.Skip("set PAPER_AGENT_LIVE_TEST=1 to call the configured Responses API")
	}
	modelID := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if modelID == "" || strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
		t.Fatal("OPENAI_MODEL and OPENAI_API_KEY are required")
	}
	client := provider.NewOpenAIClient(provider.OpenAIConfig{
		BaseURL: os.Getenv("OPENAI_BASE_URL"), APIKey: os.Getenv("OPENAI_API_KEY"),
		UserAgent: "paper-agent-live-test", MaxRetries: 1, RequestTimeout: 90 * time.Second,
	})
	summarizer, err := NewSessionSummarizer(client, modelID)
	if err != nil {
		t.Fatalf("new summarizer: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	result, err := summarizer.Summarize(ctx, session.SummaryRequest{
		SessionID: "live-test",
		Messages: []session.Message{
			{Role: "user", Content: "请分析 HiAgent 的层级工作记忆。"},
			{Role: "assistant", Content: "已确认核心问题是长任务历史不断增长，需要保留最近细节和早期阶段摘要。"},
		},
	})
	if err != nil {
		t.Fatalf("live summary: %v", err)
	}
	if strings.TrimSpace(result.Text) == "" || result.InputTokens <= 0 || result.OutputTokens <= 0 {
		t.Fatalf("unexpected live result: %#v", result)
	}
}
