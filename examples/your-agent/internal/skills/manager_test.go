package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerLoadsProjectSkillsAndChecksRequirements(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "skills")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	valid := `---
name: go-review
description: Review Go changes
metadata:
  your-agent:
    requires:
      bins: [go]
---
Run focused tests before broad tests.`
	missing := `---
name: unavailable
metadata:
  your-agent:
    requires:
      env: [YOUR_AGENT_TEST_MISSING_ENV]
---
Never loaded.`
	if err := os.WriteFile(filepath.Join(directory, "go.md"), []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "missing.md"), []byte(missing), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(root)
	if err := manager.LoadAll(); err != nil {
		t.Fatal(err)
	}
	if items := manager.List(); len(items) != 1 || items[0].Name != "go-review" || items[0].Content != "" {
		t.Fatalf("skills = %#v", items)
	}
	if skill, ok := manager.Get("go-review"); !ok || !strings.Contains(skill.Content, "focused tests") {
		t.Fatalf("skill = %#v ok=%v", skill, ok)
	}
	if len(manager.Warnings()) != 1 || !strings.Contains(manager.FormatForPrompt(), "<available_skills>") {
		t.Fatalf("warnings=%#v prompt=%q", manager.Warnings(), manager.FormatForPrompt())
	}
}
