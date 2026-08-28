package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

func (v View) NativeHistory() []domain.SessionMessage {
	result := make([]domain.SessionMessage, 0, len(v.History))
	for _, message := range v.History {
		if len(message.Blocks) == 0 {
			continue
		}
		result = append(result, domain.SessionMessage{Role: message.Role, Blocks: cloneBlocks(message.Blocks)})
	}
	return result
}

func cloneBlocks(blocks []domain.ContentBlock) []domain.ContentBlock {
	result := make([]domain.ContentBlock, len(blocks))
	for index, block := range blocks {
		result[index] = block
		result[index].Arguments = append(json.RawMessage(nil), block.Arguments...)
		result[index].Raw = append(json.RawMessage(nil), block.Raw...)
	}
	return result
}

func blocksKey(blocks []domain.ContentBlock) string {
	data, _ := json.Marshal(blocks)
	return string(data)
}

func plainTextOnly(blocks []domain.ContentBlock) bool {
	if len(blocks) == 0 {
		return true
	}
	for _, block := range blocks {
		if block.Type != domain.BlockText {
			return false
		}
	}
	return true
}

func displayText(blocks []domain.ContentBlock) string {
	var lines []string
	for _, block := range blocks {
		switch block.Type {
		case domain.BlockText:
			if strings.TrimSpace(block.Text) != "" {
				lines = append(lines, block.Text)
			}
		case domain.BlockImage:
			lines = append(lines, "[image]")
		case domain.BlockReasoning:
			if strings.TrimSpace(block.ReasoningSummary) != "" {
				lines = append(lines, "[reasoning summary] "+block.ReasoningSummary)
			} else {
				lines = append(lines, "[reasoning state preserved]")
			}
		case domain.BlockToolCall:
			lines = append(lines, fmt.Sprintf("[tool call %s] %s %s", block.ToolCallID, block.ToolName, strings.TrimSpace(string(block.Arguments))))
		case domain.BlockToolResult:
			label := "tool result"
			if block.IsError {
				label = "tool error"
			}
			lines = append(lines, fmt.Sprintf("[%s %s] %s", label, block.ToolCallID, block.Output))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func compactBlocks(blocks []domain.ContentBlock, compactToolResults bool) int {
	changed := 0
	for index := range blocks {
		switch blocks[index].Type {
		case domain.BlockText:
			if len(blocks[index].Text) > maxPromptMessageBytes {
				blocks[index].Text = compactText(blocks[index].Text, "text block")
				changed++
			}
		case domain.BlockToolResult:
			if compactToolResults && len(blocks[index].Output) > microCompactToolResultThresholdBytes && !strings.Contains(blocks[index].Output, microCompactMarker) {
				blocks[index].Output = compactToolResult(blocks[index].Output)
				changed++
			}
		}
	}
	return changed
}

func compactText(value, label string) string {
	originalBytes := len(value)
	head := truncateUTF8(value, compactedMessageHeadTail)
	tailStart := max(len(value)-compactedMessageHeadTail, 0)
	tail := value[tailStart:]
	for !utf8.ValidString(tail) && tailStart < len(value) {
		tailStart++
		tail = value[tailStart:]
	}
	return fmt.Sprintf("%s\n\n[Middle of old %s elided: %d bytes total]\n\n%s", head, label, originalBytes, tail)
}

func repairStructuredHistory(messages []Message) []Message {
	result := make([]Message, 0, len(messages))
	for index := 0; index < len(messages); {
		message := messages[index]
		if message.Role == "assistant_blocks" || message.Role == "assistant" {
			toolIDs := toolCallIDs(message.Blocks)
			if len(toolIDs) > 0 {
				if index+1 < len(messages) && messages[index+1].Role == "tool_results" && toolResultsMatch(messages[index+1].Blocks, toolIDs) {
					result = append(result, message, messages[index+1])
					index += 2
					continue
				}
				message.Blocks = blocksWithoutType(message.Blocks, domain.BlockToolCall)
				message.Content = displayText(message.Blocks)
				if len(message.Blocks) == 0 {
					index++
					continue
				}
			}
		}
		if message.Role == "tool_results" {
			message.Blocks = blocksWithoutType(message.Blocks, domain.BlockToolResult)
			message.Content = displayText(message.Blocks)
			if len(message.Blocks) == 0 {
				index++
				continue
			}
		}
		result = append(result, message)
		index++
	}
	return result
}

func toolCallIDs(blocks []domain.ContentBlock) []string {
	var result []string
	for _, block := range blocks {
		if block.Type == domain.BlockToolCall && strings.TrimSpace(block.ToolCallID) != "" {
			result = append(result, block.ToolCallID)
		}
	}
	return result
}

func toolResultsMatch(blocks []domain.ContentBlock, callIDs []string) bool {
	resultIDs := make([]string, 0, len(callIDs))
	for _, block := range blocks {
		if block.Type == domain.BlockToolResult && strings.TrimSpace(block.ToolCallID) != "" {
			resultIDs = append(resultIDs, block.ToolCallID)
		}
	}
	if len(resultIDs) != len(callIDs) {
		return false
	}
	for index := range callIDs {
		if callIDs[index] != resultIDs[index] {
			return false
		}
	}
	return true
}

func blocksWithoutType(blocks []domain.ContentBlock, blockType string) []domain.ContentBlock {
	result := blocks[:0:len(blocks)]
	for _, block := range blocks {
		if block.Type != blockType {
			result = append(result, block)
		}
	}
	return result
}
