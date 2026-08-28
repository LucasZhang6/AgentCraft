package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

type legacyMessage struct {
	id         int64
	sessionID  string
	role       string
	content    string
	blocksJSON string
	createdAt  string
}

// backfillLegacyTurns upgrades pre-canonical messages, including messages copied
// by Fork, into completed turns without flattening any blocks already present.
func (s *Store) backfillLegacyTurns(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, session_id, role, content, blocks_json, created_at
FROM session_messages WHERE turn_id = '' ORDER BY session_id, id`)
	if err != nil {
		return fmt.Errorf("query legacy session messages: %w", err)
	}
	var messages []legacyMessage
	for rows.Next() {
		var item legacyMessage
		if err := rows.Scan(&item.id, &item.sessionID, &item.role, &item.content, &item.blocksJSON, &item.createdAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan legacy session message: %w", err)
		}
		messages = append(messages, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate legacy session messages: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close legacy session messages: %w", err)
	}
	if len(messages) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy turn backfill: %w", err)
	}
	defer tx.Rollback()

	for start := 0; start < len(messages); {
		end := start + 1
		for end < len(messages) && messages[end].sessionID == messages[start].sessionID {
			if strings.EqualFold(messages[end].role, "user") {
				break
			}
			end++
		}
		if err := backfillLegacyTurn(ctx, tx, messages[start:end]); err != nil {
			return err
		}
		start = end
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit legacy turn backfill: %w", err)
	}
	return nil
}

func backfillLegacyTurn(ctx context.Context, tx *sql.Tx, messages []legacyMessage) error {
	if len(messages) == 0 {
		return nil
	}
	sessionID := messages[0].sessionID
	turnID := legacyTurnID(sessionID, messages[0].id)
	startedAt := normalizedLegacyTime(messages[0].createdAt)
	committedAt := normalizedLegacyTime(messages[len(messages)-1].createdAt)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO session_turns(id, session_id, run_id, status, error, started_at, committed_at)
VALUES(?, ?, 'legacy-import', ?, '', ?, ?)`, turnID, sessionID, TurnCompleted, startedAt, committedAt); err != nil {
		return fmt.Errorf("insert legacy session turn: %w", err)
	}
	var sequence int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM session_turn_events WHERE session_id = ?`, sessionID,
	).Scan(&sequence); err != nil {
		return fmt.Errorf("allocate legacy event sequence: %w", err)
	}
	for index, message := range messages {
		blocks := legacyBlocks(message)
		blocksJSON, err := json.Marshal(blocks)
		if err != nil {
			return fmt.Errorf("encode legacy message blocks: %w", err)
		}
		content := message.content
		if strings.TrimSpace(content) == "" {
			content = displayText(blocks)
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE session_messages SET turn_id = ?, sequence = ?, blocks_json = ?, content = ? WHERE id = ?`,
			turnID, sequence, string(blocksJSON), content, message.id); err != nil {
			return fmt.Errorf("upgrade legacy session message: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO session_turn_events
(session_id, turn_id, sequence, event_index, kind, role, content, blocks_json, payload_json, tool_call_id, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, sessionID, turnID, sequence, index, TurnEventMessage,
			message.role, content, string(blocksJSON), string(blocksJSON), firstToolCallID(blocks), normalizedLegacyTime(message.createdAt)); err != nil {
			return fmt.Errorf("insert legacy canonical message: %w", err)
		}
		sequence++
	}
	statusPayload, _ := json.Marshal(map[string]string{"status": string(TurnCompleted), "source": "legacy-import"})
	if _, err := tx.ExecContext(ctx, `
INSERT INTO session_turn_events
(session_id, turn_id, sequence, event_index, kind, content, payload_json, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, sessionID, turnID, sequence, len(messages), TurnEventStatus,
		string(TurnCompleted), string(statusPayload), committedAt); err != nil {
		return fmt.Errorf("insert legacy canonical status: %w", err)
	}
	return nil
}

func legacyBlocks(message legacyMessage) []domain.ContentBlock {
	if strings.TrimSpace(message.blocksJSON) != "" {
		var blocks []domain.ContentBlock
		if json.Unmarshal([]byte(message.blocksJSON), &blocks) == nil && len(blocks) > 0 {
			return cloneBlocks(blocks)
		}
	}
	if strings.TrimSpace(message.content) == "" {
		return nil
	}
	return []domain.ContentBlock{{Type: domain.BlockText, Text: message.content}}
}

func legacyTurnID(sessionID string, firstMessageID int64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("legacy:%s:%d", sessionID, firstMessageID)))
	return "turn_legacy_" + hex.EncodeToString(digest[:12])
}

func normalizedLegacyTime(value string) string {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC().Format(time.RFC3339Nano)
	}
	return time.Unix(0, 0).UTC().Format(time.RFC3339Nano)
}
