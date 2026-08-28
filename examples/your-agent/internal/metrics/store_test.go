package metrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/metrics"
)

func TestStoreCalculatesTaskSuccessRate(t *testing.T) {
	store, err := metrics.NewStore(t.TempDir() + "/metrics.db")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	for _, test := range []struct {
		runID   string
		success bool
	}{
		{runID: "run-1", success: true},
		{runID: "run-2", success: false},
	} {
		recorder := store.Bind(test.runID)
		if err := recorder.Record(ctx, domain.RunMetrics{
			StartedAt: now, CompletedAt: now.Add(time.Second), DurationMS: 1000,
			TotalTokens: 10, ToolCalls: 2, Success: test.success,
		}); err != nil {
			t.Fatalf("record %s: %v", test.runID, err)
		}
	}
	summary, err := store.Summary(ctx)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.Runs != 2 || summary.SuccessfulRuns != 1 || summary.TaskSuccessRate != 0.5 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.TotalTokens != 20 || summary.TotalToolCalls != 4 {
		t.Fatalf("totals = %#v", summary)
	}
}
