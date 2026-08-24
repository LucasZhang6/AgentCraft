package session

import (
	"context"
	"os"
	"strings"
	"time"
)

type SummaryRequest struct {
	SessionID string
	Messages  []Message
}

type SummaryResult struct {
	Text         string
	InputTokens  int
	OutputTokens int
}

type Summarizer interface {
	Summarize(context.Context, SummaryRequest) (SummaryResult, error)
}

type summaryState int

const (
	summaryIdle summaryState = iota
	summaryRunning
	summaryReady
)

type summaryCache struct {
	state       summaryState
	text        string
	coveredUpto int
	generation  uint64
}

func (s *Store) maybePrewarmSummary(ctx context.Context, sessionID string) {
	if s == nil || !sessionSummaryEnabled() {
		return
	}
	summarizer := s.currentSummarizer()
	if summarizer == nil {
		return
	}
	messages, err := s.Messages(ctx, sessionID)
	if err != nil || !s.shouldPrewarm(ctx, messages, sessionID) {
		return
	}

	// Predict the next user turn so the summary covers the exact prefix that the
	// next BuildPrompt call is likely to remove.
	predicted := append(cloneMessages(messages), Message{SessionID: sessionID, Role: "user", Content: "[next user turn]"})
	cutIndex := findCutIndex(predicted, s.config.MinRecentTurns)
	if cutIndex <= 0 || cutIndex > len(messages) {
		return
	}
	snapshot := cloneMessages(messages[:cutIndex])
	generation, started := s.beginSummary(sessionID, cutIndex)
	if !started {
		return
	}

	go func() {
		defer s.wg.Done()
		timeoutCtx, cancel := context.WithTimeout(s.ctx, s.config.SummaryTimeout)
		defer cancel()
		startedAt := time.Now()
		result, err := summarizer.Summarize(timeoutCtx, SummaryRequest{
			SessionID: sessionID,
			Messages:  snapshot,
		})
		s.recordSummaryUsage(sessionID, result, time.Since(startedAt))
		if err != nil {
			s.finishSummary(sessionID, generation, cutIndex, "")
			return
		}
		s.finishSummary(sessionID, generation, cutIndex, result.Text)
	}()
}

func (s *Store) SetSummarizer(summarizer Summarizer) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summarizer = summarizer
	for _, cache := range s.summaries {
		cache.generation++
	}
	s.summaries = make(map[string]*summaryCache)
}

func (s *Store) currentSummarizer() Summarizer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summarizer
}

func (s *Store) shouldPrewarm(ctx context.Context, messages []Message, sessionID string) bool {
	ratio := s.config.SummaryPrewarmRatio
	if totalBytes(messages) >= int(float64(s.config.TriggerBytes)*ratio) {
		return true
	}
	tokens, err := s.lastInputTokens(ctx, sessionID)
	return err == nil && tokens > 0 && tokens >= int(float64(s.config.TriggerTokens)*ratio)
}

func (s *Store) beginSummary(sessionID string, covered int) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ctx.Err() != nil {
		return 0, false
	}
	cache := s.summaries[sessionID]
	if cache == nil {
		cache = &summaryCache{}
		s.summaries[sessionID] = cache
	}
	if (cache.state == summaryRunning || cache.state == summaryReady) && cache.coveredUpto >= covered {
		return cache.generation, false
	}
	cache.generation++
	cache.state = summaryRunning
	cache.text = ""
	cache.coveredUpto = covered
	s.wg.Add(1)
	return cache.generation, true
}

func (s *Store) finishSummary(sessionID string, generation uint64, covered int, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache := s.summaries[sessionID]
	if cache == nil || cache.generation != generation {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		cache.state = summaryIdle
		cache.text = ""
		cache.coveredUpto = 0
		return
	}
	cache.state = summaryReady
	cache.text = value
	cache.coveredUpto = covered
}

func (s *Store) summaryFor(sessionID string, cutIndex int) (string, string, int, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache := s.summaries[sessionID]
	if cache == nil {
		return "", "idle", 0, 0
	}
	state := cache.state.String()
	if cache.state != summaryReady || cutIndex > cache.coveredUpto {
		return "", state, cache.coveredUpto, cache.generation
	}
	return cache.text, state, cache.coveredUpto, cache.generation
}

func (s *Store) consumeSummary(sessionID string, generation uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache := s.summaries[sessionID]
	if cache == nil || cache.generation != generation {
		return
	}
	cache.generation++
	cache.state = summaryIdle
	cache.text = ""
	cache.coveredUpto = 0
}

func (s *Store) recordSummaryUsage(sessionID string, result SummaryResult, elapsed time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(ctx, `
UPDATE sessions
SET summary_calls = summary_calls + 1,
    summary_input_tokens = summary_input_tokens + ?,
    summary_output_tokens = summary_output_tokens + ?,
    summary_latency_ms = summary_latency_ms + ?
WHERE id = ?`, max(result.InputTokens, 0), max(result.OutputTokens, 0), elapsed.Milliseconds(), sessionID)
}

func (s *Store) invalidateSummary(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache := s.summaries[sessionID]
	if cache != nil {
		cache.generation++
	}
	delete(s.summaries, sessionID)
}

func (s *Store) summaryStatus(sessionID string) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache := s.summaries[sessionID]
	if cache == nil {
		return "idle", 0
	}
	return cache.state.String(), cache.coveredUpto
}

func (state summaryState) String() string {
	switch state {
	case summaryRunning:
		return "running"
	case summaryReady:
		return "ready"
	default:
		return "idle"
	}
}

func sessionSummaryEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PAPER_AGENT_SESSION_LLM_SUMMARY"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func sanitizeSummary(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "</conversation_summary>", "[end conversation summary]")
	return truncateUTF8(value, 4096)
}
