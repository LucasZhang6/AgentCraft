package verification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/planning"
)

const gateStepPrefix = "verification_gate_"

var verificationCommandPattern = regexp.MustCompile(`(?i)(^|\s|&&|\|\||;)(go test|go vet|go build|npm (run )?([a-z0-9:_-]*test[a-z0-9:_-]*|lint|build|typecheck)|npx playwright test|pnpm (run )?([a-z0-9:_-]*test[a-z0-9:_-]*|lint|build|typecheck)|yarn (run )?([a-z0-9:_-]*test[a-z0-9:_-]*|lint|build|typecheck)|bun (run )?([a-z0-9:_-]*test[a-z0-9:_-]*|lint|build|typecheck)|playwright test|cypress run|pytest|python -m (pytest|py_compile)|gradle(w)? (test|build|check|compile|compileKotlin)|\./gradlew (test|build|check|compile|compileKotlin)|mvn (test|compile)|cargo (test|check|clippy)|make (test|lint|check|build)|git diff --check|tsc|eslint|ruff|mypy)(\s|$)`)

type Snapshot struct {
	MaterialWork bool     `json:"materialWork"`
	Verified     bool     `json:"verified"`
	TouchedFiles []string `json:"touchedFiles"`
	Command      string   `json:"recommendedCommand,omitempty"`
	Forced       int      `json:"forced"`
}

type Gate struct {
	mu       sync.Mutex
	workDir  string
	touched  map[string]struct{}
	material bool
	verified bool
	forced   int
}

func New(workDir string) *Gate {
	root, err := filepath.Abs(strings.TrimSpace(workDir))
	if err != nil || strings.TrimSpace(workDir) == "" {
		root, _ = os.Getwd()
	}
	return &Gate{workDir: root, touched: make(map[string]struct{})}
}

func (gate *Gate) ObserveTool(_ context.Context, name string, args map[string]any, _ domain.ToolExecution, toolErr error) {
	if gate == nil || toolErr != nil {
		return
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	switch name {
	case "file_write", "file_edit":
		path, _ := args["path"].(string)
		if strings.TrimSpace(path) == "" {
			return
		}
		gate.material = true
		if !filepath.IsAbs(path) {
			path = filepath.Join(gate.workDir, path)
		}
		gate.touched[filepath.Clean(path)] = struct{}{}
	case "bash":
		command, _ := args["command"].(string)
		if verificationCommandPattern.MatchString(strings.TrimSpace(command)) {
			gate.verified = true
		}
	}
}

func (gate *Gate) Ensure(ctx context.Context, store *planning.Store, item planning.Plan) (planning.Plan, bool, error) {
	if gate == nil || store == nil {
		return item, false, nil
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	for _, step := range item.Steps {
		if (step.Tool == "file_write" || step.Tool == "file_edit") && step.Status == domain.PlanCompleted {
			gate.material = true
		}
	}
	if !gate.material || gate.verified {
		return item, false, nil
	}
	gateSteps := 0
	for _, step := range item.Steps {
		if strings.HasPrefix(step.ID, gateStepPrefix) {
			gateSteps++
			if step.Status == domain.PlanCompleted && gate.forced > 0 {
				return item, false, errors.New("verification gate step completed without successful verification evidence")
			}
			if step.Status != domain.PlanCompleted && step.Status != domain.PlanSkipped {
				return item, false, nil
			}
		}
	}
	if gate.forced >= 1 {
		return item, false, errors.New("verification gate could not obtain test, build, lint, type-check, or runtime evidence")
	}
	command := recommendCommand(gate.workDir, gate.touched)
	dependencies := make([]string, 0, len(item.Steps))
	for _, step := range item.Steps {
		if step.Status == domain.PlanCompleted || step.Status == domain.PlanSkipped {
			dependencies = append(dependencies, step.ID)
		}
	}
	gate.forced++
	item.Steps = append(item.Steps, domain.PlanStep{
		ID: fmt.Sprintf("%s%d", gateStepPrefix, gateSteps+1), AgentRole: "verifier", Tool: "bash", Status: domain.PlanPending,
		Description:  fmt.Sprintf("Run the required verification command `%s`. Use the bash tool and report its real output; do not claim success without executing it.", command),
		Dependencies: dependencies, SuccessCriteria: "the recommended verification command exits successfully",
		Evidence: []string{"host verification gate required after material file changes"},
	})
	updated, err := store.Update(ctx, item.ID, item.Steps)
	return updated, err == nil, err
}

func (gate *Gate) Snapshot() Snapshot {
	if gate == nil {
		return Snapshot{}
	}
	gate.mu.Lock()
	defer gate.mu.Unlock()
	files := make([]string, 0, len(gate.touched))
	for path := range gate.touched {
		files = append(files, path)
	}
	sort.Strings(files)
	return Snapshot{MaterialWork: gate.material, Verified: gate.verified, TouchedFiles: files, Command: recommendCommand(gate.workDir, gate.touched), Forced: gate.forced}
}

func recommendCommand(root string, touched map[string]struct{}) string {
	files := make([]string, 0, len(touched))
	for path := range touched {
		files = append(files, strings.ToLower(path))
	}
	switch {
	case exists(filepath.Join(root, "go.mod")) && containsSuffix(files, ".go"):
		return "go test ./..."
	case exists(filepath.Join(root, "package.json")) && containsAnySuffix(files, ".js", ".jsx", ".ts", ".tsx", ".vue", ".svelte"):
		return nodeVerification(root)
	case exists(filepath.Join(root, "Cargo.toml")) && containsSuffix(files, ".rs"):
		return "cargo test"
	case containsSuffix(files, ".py"):
		return "python -m pytest"
	case exists(filepath.Join(root, "Makefile")):
		return "make test"
	default:
		return "git diff --check"
	}
}

func nodeVerification(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err == nil {
		var manifest struct {
			Scripts map[string]string `json:"scripts"`
		}
		_ = json.Unmarshal(data, &manifest)
		for _, script := range []string{"test", "typecheck", "lint", "build"} {
			if strings.TrimSpace(manifest.Scripts[script]) != "" {
				return "npm run " + script
			}
		}
	}
	return "npm test"
}

func containsSuffix(files []string, suffix string) bool { return containsAnySuffix(files, suffix) }

func containsAnySuffix(files []string, suffixes ...string) bool {
	for _, file := range files {
		for _, suffix := range suffixes {
			if strings.HasSuffix(file, suffix) {
				return true
			}
		}
	}
	return false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
