package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

const (
	microCompactToolResultThresholdBytes = 16 * 1024
	microCompactMarker                   = "[tool_result micro-compacted:"
	supersededToolResultPrefix           = "[tool_result superseded by later "
)

type DedupeInfo struct {
	FoldedResults int `json:"foldedResults"`
	BytesSaved    int `json:"bytesSaved"`
}

// PrepareNativeHistory applies the same model-view housekeeping used by the
// durable Session store to an in-flight ReAct history. It never mutates the
// caller's slice or the SQLite source of truth.
func PrepareNativeHistory(history []domain.SessionMessage, force bool, minRecentTurns int) ([]domain.SessionMessage, Info) {
	messages := make([]Message, 0, len(history))
	for _, item := range history {
		blocks := cloneBlocks(item.Blocks)
		messages = append(messages, Message{Role: item.Role, Content: displayText(blocks), Blocks: blocks})
	}
	bytesBefore := totalBytes(messages)
	microCount := microCompact(messages)
	dedupe := dedupeStaleResults(messages)
	info := Info{
		Forced: force, BytesBefore: bytesBefore, MicroCompactions: microCount,
		DedupeFoldedResults: dedupe.FoldedResults, DedupeBytesSaved: dedupe.BytesSaved,
		RetainedTurns: countUserTurns(messages),
	}
	if force {
		info.TriggeredBy = "context_limit"
	} else if totalBytes(messages) > DefaultTriggerBytes {
		info.TriggeredBy = "bytes"
	}
	if info.TriggeredBy != "" {
		cutIndex := findCutIndex(messages, max(minRecentTurns, 0))
		if cutIndex > 0 {
			dropped := cloneMessages(messages[:cutIndex])
			content := fmt.Sprintf(
				"[Earlier conversation truncated for context length: %d earlier messages elided. Continue from the most recent user request below.]\n\n<session_digest>\n%s\n</session_digest>",
				len(dropped), digest(dropped, 4000),
			)
			messages = append([]Message{{
				Role: "user", Content: content,
				Blocks: []domain.ContentBlock{{Type: domain.BlockText, Text: content}},
			}}, messages[cutIndex:]...)
			info.Compacted = true
			info.DroppedMessages = len(dropped)
			info.RetainedTurns = countUserTurns(messages) - 1
		}
	}
	messages = repairStructuredHistory(messages)
	info.BytesAfter = totalBytes(messages)
	result := make([]domain.SessionMessage, 0, len(messages))
	for _, message := range messages {
		if len(message.Blocks) == 0 {
			continue
		}
		result = append(result, domain.SessionMessage{Role: message.Role, Blocks: cloneBlocks(message.Blocks)})
	}
	return result, info
}

func dedupeStaleResults(messages []Message) DedupeInfo {
	useFingerprints := make(map[string]string)
	latestByFingerprint := make(map[string]string)
	for _, message := range messages {
		if message.Role != "assistant" && message.Role != "assistant_blocks" {
			continue
		}
		for _, block := range message.Blocks {
			if block.Type != domain.BlockToolCall || strings.TrimSpace(block.ToolCallID) == "" {
				continue
			}
			fingerprint := toolFingerprint(block.ToolName, block.Arguments)
			if fingerprint == "" {
				continue
			}
			useFingerprints[block.ToolCallID] = fingerprint
			latestByFingerprint[fingerprint] = block.ToolCallID
		}
	}
	var info DedupeInfo
	for messageIndex := range messages {
		if messages[messageIndex].Role != "tool_results" {
			continue
		}
		for blockIndex := range messages[messageIndex].Blocks {
			block := &messages[messageIndex].Blocks[blockIndex]
			if block.Type != domain.BlockToolResult {
				continue
			}
			fingerprint, ok := useFingerprints[block.ToolCallID]
			if !ok || latestByFingerprint[fingerprint] == block.ToolCallID || strings.HasPrefix(block.Output, supersededToolResultPrefix) {
				continue
			}
			before := len(block.Output)
			toolName, short := splitToolFingerprint(fingerprint)
			replacement := fmt.Sprintf("%s%s(%s); %d bytes elided]", supersededToolResultPrefix, toolName, short, before)
			if len(replacement) >= before {
				continue
			}
			block.Output = replacement
			messages[messageIndex].Content = displayText(messages[messageIndex].Blocks)
			info.FoldedResults++
			info.BytesSaved += before - len(block.Output)
		}
	}
	return info
}

func toolFingerprint(name string, raw json.RawMessage) string {
	name = strings.TrimSpace(name)
	if !dedupeTool(name) || len(raw) == 0 || !json.Valid(raw) {
		return ""
	}
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil || args == nil {
		return ""
	}
	stable, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return name + ":" + string(stable)
}

func dedupeTool(name string) bool {
	switch name {
	case "bash", "file_read", "glob", "grep", "list_dir", "semantic_code_search",
		"web_search", "web_fetch", "search_papers", "read_paper_card", "skill":
		return true
	default:
		return false
	}
}

func splitToolFingerprint(value string) (string, string) {
	index := strings.IndexByte(value, ':')
	if index < 0 {
		return value, ""
	}
	short := value[index+1:]
	if len(short) > 80 {
		short = short[:80] + "..."
	}
	return value[:index], short
}

func lastToolResultMessageIndex(messages []Message) int {
	for index := len(messages) - 1; index >= 0; index-- {
		for _, block := range messages[index].Blocks {
			if block.Type == domain.BlockToolResult {
				return index
			}
		}
	}
	return -1
}

func compactToolResult(value string) string {
	if len(value) <= microCompactToolResultThresholdBytes || strings.Contains(value, microCompactMarker) {
		return value
	}
	head := truncateUTF8(value, compactedMessageHeadTail)
	tailStart := max(len(value)-compactedMessageHeadTail, 0)
	tail := value[tailStart:]
	for len(tail) > 0 && !utf8.ValidString(tail) && tailStart < len(value) {
		tailStart++
		tail = value[tailStart:]
	}
	omitted := max(len(value)-len(head)-len(tail), 0)
	return fmt.Sprintf("%s original=%d bytes, omitted=%d bytes; kept head=%d and tail=%d. Re-run a narrower tool query if needed.]\n%s\n\n...\n\n%s",
		microCompactMarker, len(value), omitted, len(head), len(tail), head, tail)
}
