package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
)

type ExecuteFunc func(context.Context, map[string]any) (any, error)

type Definition struct {
	Name           string
	Description    string
	Risk           string
	Schema         domain.ToolSchema
	InputSchema    json.RawMessage
	Timeout        time.Duration
	MaxOutputBytes int
	Execute        ExecuteFunc
}

type ApprovalRequest struct {
	Tool string
	Risk string
	Args map[string]any
}

type ApprovalFunc func(context.Context, ApprovalRequest) (bool, error)

type RegistryOptions struct {
	DefaultTimeout time.Duration
	MaxOutputBytes int
	Approval       ApprovalFunc
}

type Registry struct {
	tools          map[string]Definition
	defaultTimeout time.Duration
	maxOutputBytes int
	approval       ApprovalFunc
}

func NewRegistry(options ...RegistryOptions) *Registry {
	config := RegistryOptions{DefaultTimeout: 30 * time.Second, MaxOutputBytes: 64 * 1024}
	if len(options) > 0 {
		if options[0].DefaultTimeout > 0 {
			config.DefaultTimeout = options[0].DefaultTimeout
		}
		if options[0].MaxOutputBytes > 0 {
			config.MaxOutputBytes = options[0].MaxOutputBytes
		}
		config.Approval = options[0].Approval
	}
	return &Registry{
		tools:          make(map[string]Definition),
		defaultTimeout: config.DefaultTimeout,
		maxOutputBytes: config.MaxOutputBytes,
		approval:       config.Approval,
	}
}

func (r *Registry) Register(definition Definition) error {
	if definition.Name == "" || definition.Execute == nil {
		return errors.New("a tool needs a name and execute function")
	}
	if _, exists := r.tools[definition.Name]; exists {
		return fmt.Errorf("tool already registered: %s", definition.Name)
	}
	risk, err := normalizeRisk(definition.Risk)
	if err != nil {
		return fmt.Errorf("register tool %s: %w", definition.Name, err)
	}
	definition.Risk = risk
	r.tools[definition.Name] = definition
	return nil
}

func (r *Registry) Descriptions() []domain.ToolDescription {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]domain.ToolDescription, 0, len(names))
	for _, name := range names {
		definition := r.tools[name]
		result = append(result, domain.ToolDescription{
			Name:        definition.Name,
			Description: definition.Description,
			Schema:      definition.Schema,
			InputSchema: definition.InputSchema,
			Risk:        definition.Risk,
		})
	}
	return result
}

func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (domain.ToolExecution, error) {
	started := time.Now()
	definition, exists := r.tools[name]
	if !exists {
		return domain.ToolExecution{}, fmt.Errorf("tool is not allowlisted: %s", name)
	}
	if err := validateDefinitionArgs(definition, args); err != nil {
		return domain.ToolExecution{}, err
	}
	execution := domain.ToolExecution{}
	if definition.Risk == domain.RiskWrite || definition.Risk == domain.RiskDangerous {
		execution.ApprovalRequested = true
		if r.approval == nil {
			return execution, fmt.Errorf("tool %s requires human approval", name)
		}
		approved, err := r.approval(ctx, ApprovalRequest{Tool: name, Risk: definition.Risk, Args: args})
		if err != nil {
			return execution, fmt.Errorf("approve tool %s: %w", name, err)
		}
		if !approved {
			return execution, fmt.Errorf("tool %s was rejected by the user", name)
		}
	}

	timeout := definition.Timeout
	if timeout <= 0 {
		timeout = r.defaultTimeout
	}
	toolContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type outcome struct {
		result any
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := definition.Execute(toolContext, args)
		done <- outcome{result: result, err: err}
	}()

	var result any
	select {
	case <-toolContext.Done():
		execution.DurationMS = time.Since(started).Milliseconds()
		return execution, fmt.Errorf("tool %s timed out after %s: %w", name, timeout, toolContext.Err())
	case completed := <-done:
		if completed.err != nil {
			execution.DurationMS = time.Since(started).Milliseconds()
			return execution, completed.err
		}
		result = completed.result
	}

	limit := definition.MaxOutputBytes
	if limit <= 0 {
		limit = r.maxOutputBytes
	}
	limited, outputBytes, truncated, err := limitOutput(result, limit)
	execution.DurationMS = time.Since(started).Milliseconds()
	execution.OutputBytes = outputBytes
	execution.Truncated = truncated
	execution.Result = limited
	if err != nil {
		return execution, fmt.Errorf("tool %s output: %w", name, err)
	}
	return execution, nil
}

func validateDefinitionArgs(definition Definition, args map[string]any) error {
	if len(definition.InputSchema) == 0 {
		return validateArgs(definition.Schema, args)
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		return fmt.Errorf("tool %s has invalid JSON schema: %w", definition.Name, err)
	}
	converted := domain.ToolSchema{Type: "object", Required: schema.Required, Properties: map[string]domain.ToolField{}}
	for name, property := range schema.Properties {
		converted.Properties[name] = domain.ToolField{Type: property.Type}
	}
	return validateArgs(converted, args)
}

func normalizeRisk(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "read", "read-only", "readonly":
		return domain.RiskRead, nil
	case "write":
		return domain.RiskWrite, nil
	case "dangerous", "high":
		return domain.RiskDangerous, nil
	default:
		return "", fmt.Errorf("unsupported risk level %q", value)
	}
}

func limitOutput(value any, limit int) (any, int, bool, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, 0, false, fmt.Errorf("encode result: %w", err)
	}
	if limit <= 0 || len(data) <= limit {
		return value, len(data), false, nil
	}
	switch typed := value.(type) {
	case string:
		return truncateStringToJSONLimit(typed, limit), len(data), true, nil
	case []byte:
		return truncateBytesToJSONLimit(typed, limit), len(data), true, nil
	default:
		return nil, len(data), true, fmt.Errorf("structured result is %d bytes, limit is %d", len(data), limit)
	}
}

func truncateStringToJSONLimit(value string, limit int) string {
	low, high := 0, min(len(value), limit)
	best := ""
	for low <= high {
		middle := low + (high-low)/2
		candidate := truncateUTF8(value, middle)
		data, _ := json.Marshal(candidate)
		if len(data) <= limit {
			best = candidate
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	return best
}

func truncateBytesToJSONLimit(value []byte, limit int) []byte {
	low, high := 0, min(len(value), limit)
	best := []byte(nil)
	for low <= high {
		middle := low + (high-low)/2
		candidate := value[:middle]
		data, _ := json.Marshal(candidate)
		if len(data) <= limit {
			best = append(best[:0], candidate...)
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	return best
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) && len(value) > 0 {
		value = value[:len(value)-1]
	}
	return value
}

func validateArgs(schema domain.ToolSchema, args map[string]any) error {
	if args == nil {
		return errors.New("tool arguments must be an object")
	}
	for _, field := range schema.Required {
		if _, exists := args[field]; !exists {
			return fmt.Errorf("missing required tool argument: %s", field)
		}
	}
	for field, value := range args {
		property, exists := schema.Properties[field]
		if !exists {
			return fmt.Errorf("unknown tool argument: %s", field)
		}
		if !matchesType(property.Type, value) {
			return fmt.Errorf("tool argument %s must be a %s", field, property.Type)
		}
	}
	return nil
}

func matchesType(expected string, value any) bool {
	switch expected {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		if value == nil {
			return false
		}
		switch reflect.TypeOf(value).Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			return true
		default:
			return false
		}
	case "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}
