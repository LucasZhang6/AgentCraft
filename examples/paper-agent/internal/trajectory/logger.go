package trajectory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
)

var sensitiveKey = regexp.MustCompile(`(?i)key|token|secret|password`)

type OnEvent func(domain.Event)

type Logger struct {
	path    string
	onEvent OnEvent
	mu      sync.Mutex
}

func NewLogger(path string, onEvent OnEvent) *Logger {
	return &Logger{path: path, onEvent: onEvent}
}

func (l *Logger) Record(ctx context.Context, eventType string, payload any) (domain.Event, error) {
	if err := ctx.Err(); err != nil {
		return domain.Event{}, err
	}
	clean, err := sanitize(payload)
	if err != nil {
		return domain.Event{}, fmt.Errorf("sanitize event: %w", err)
	}
	event := domain.Event{
		Timestamp: time.Now().UTC(),
		Type:      eventType,
		Payload:   clean,
	}
	data, err := json.Marshal(event)
	if err != nil {
		return domain.Event{}, fmt.Errorf("encode event: %w", err)
	}
	data = append(data, '\n')

	if err := l.append(data); err != nil {
		return domain.Event{}, err
	}
	if l.onEvent != nil {
		l.onEvent(event)
	}
	return event, nil
}

func (l *Logger) append(data []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("create run directory: %w", err)
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open run log: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("append run log: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close run log: %w", err)
	}
	return nil
}

func sanitize(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	return sanitizeValue(decoded), nil
}

func sanitizeValue(value any) any {
	switch typed := value.(type) {
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = sanitizeValue(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if sensitiveKey.MatchString(key) {
				result[key] = "[REDACTED]"
			} else {
				result[key] = sanitizeValue(item)
			}
		}
		return result
	default:
		return value
	}
}
