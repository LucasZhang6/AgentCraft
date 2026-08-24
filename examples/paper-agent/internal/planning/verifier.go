package planning

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
)

type Verification struct {
	Passed         bool     `json:"passed"`
	NeedsHuman     bool     `json:"needsHuman"`
	Evidence       []string `json:"evidence"`
	FailureMessage string   `json:"failureMessage,omitempty"`
}

type Verifier struct{ WorkDir string }

func (verifier Verifier) Verify(step domain.PlanStep, output string) Verification {
	result := Verification{Passed: true}
	for _, check := range step.Acceptance {
		switch strings.ToLower(strings.TrimSpace(check.Type)) {
		case "human":
			result.Passed = false
			result.NeedsHuman = true
			result.Evidence = append(result.Evidence, "waiting for human acceptance")
		case "output_contains":
			if !strings.Contains(output, check.Expected) {
				return failedVerification(fmt.Sprintf("output does not contain %q", check.Expected), result.Evidence)
			}
			result.Evidence = append(result.Evidence, "output contains "+check.Expected)
		case "file_exists", "file_contains":
			path, err := verifier.workspacePath(check.Path)
			if err != nil {
				return failedVerification(err.Error(), result.Evidence)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return failedVerification(err.Error(), result.Evidence)
			}
			if check.Type == "file_contains" && !strings.Contains(string(data), check.Expected) {
				return failedVerification(fmt.Sprintf("%s does not contain %q", check.Path, check.Expected), result.Evidence)
			}
			result.Evidence = append(result.Evidence, check.Type+":"+check.Path)
		default:
			return failedVerification("unsupported acceptance check: "+check.Type, result.Evidence)
		}
	}
	if len(step.Acceptance) == 0 {
		result.Evidence = append(result.Evidence, "executor completed: "+step.SuccessCriteria)
	}
	return result
}

func (verifier Verifier) workspacePath(path string) (string, error) {
	root := verifier.WorkDir
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, path))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("verification path escapes workspace")
	}
	return target, nil
}

func failedVerification(message string, evidence []string) Verification {
	return Verification{FailureMessage: message, Evidence: evidence}
}
