package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

func TestPrepareNativeHistoryMicroCompactsOldToolResultButKeepsLatest(t *testing.T) {
	oldOutput := strings.Repeat("old-result-", 1900)
	latestOutput := strings.Repeat("latest-result-", 1700)
	history := []domain.SessionMessage{
		textMessage("user", "first request"),
		toolCallMessage("call_old", "file_read", `{"path":"old.txt"}`),
		toolResultMessage("call_old", oldOutput),
		textMessage("user", "second request"),
		toolCallMessage("call_latest", "file_read", `{"path":"latest.txt"}`),
		toolResultMessage("call_latest", latestOutput),
	}

	prepared, info := PrepareNativeHistory(history, false, DefaultMinRecentTurns)
	if info.MicroCompactions != 1 || len(prepared) != len(history) {
		t.Fatalf("info=%#v messages=%d", info, len(prepared))
	}
	if !strings.Contains(prepared[2].Blocks[0].Output, microCompactMarker) {
		t.Fatalf("old result was not compacted: %q", prepared[2].Blocks[0].Output)
	}
	if prepared[5].Blocks[0].Output != latestOutput {
		t.Fatal("latest tool result must remain byte-for-byte intact")
	}
	if history[2].Blocks[0].Output != oldOutput {
		t.Fatal("PrepareNativeHistory mutated its caller-owned history")
	}
}

func TestPrepareNativeHistoryFoldsSupersededResultsWithoutBreakingPairs(t *testing.T) {
	args := `{"path":"same.txt"}`
	history := []domain.SessionMessage{
		textMessage("user", "first request"),
		toolCallMessage("call_old", "file_read", args),
		toolResultMessage("call_old", strings.Repeat("old contents ", 100)),
		textMessage("user", "read it again"),
		toolCallMessage("call_new", "file_read", args),
		toolResultMessage("call_new", "new contents"),
	}

	prepared, info := PrepareNativeHistory(history, false, DefaultMinRecentTurns)
	if info.DedupeFoldedResults != 1 || info.DedupeBytesSaved <= 0 {
		t.Fatalf("dedupe info = %#v", info)
	}
	if !strings.HasPrefix(prepared[2].Blocks[0].Output, supersededToolResultPrefix) {
		t.Fatalf("old result = %q", prepared[2].Blocks[0].Output)
	}
	if prepared[4].Blocks[0].ToolCallID != "call_new" || prepared[5].Blocks[0].ToolCallID != "call_new" || prepared[5].Blocks[0].Output != "new contents" {
		t.Fatalf("latest call/result pair changed: %#v %#v", prepared[4], prepared[5])
	}
}

func textMessage(role, value string) domain.SessionMessage {
	return domain.SessionMessage{Role: role, Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: value}}}
}

func toolCallMessage(id, name, args string) domain.SessionMessage {
	return domain.SessionMessage{Role: "assistant_blocks", Blocks: []domain.ContentBlock{{
		Type: domain.BlockToolCall, ToolCallID: id, ToolName: name, Arguments: json.RawMessage(args),
	}}}
}

func toolResultMessage(id, output string) domain.SessionMessage {
	return domain.SessionMessage{Role: "tool_results", Blocks: []domain.ContentBlock{{
		Type: domain.BlockToolResult, ToolCallID: id, Output: output,
	}}}
}
