package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	maxSkillBytes       = 32 * 1024
	maxPromptSkillBytes = 128 * 1024
)

type Skill struct {
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description" yaml:"description"`
	Content     string   `json:"content,omitempty" yaml:"-"`
	Path        string   `json:"path" yaml:"-"`
	Requires    Requires `json:"requires" yaml:"-"`
}

type Requires struct {
	Bins []string `json:"bins,omitempty" yaml:"bins"`
	Env  []string `json:"env,omitempty" yaml:"env"`
}

type metadata struct {
	YourAgent runtimeMetadata `yaml:"your-agent"`
	Verdent   runtimeMetadata `yaml:"verdent"`
}

type runtimeMetadata struct {
	Requires Requires `yaml:"requires"`
}

type frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Metadata    metadata `yaml:"metadata"`
}

type Manager struct {
	mu       sync.RWMutex
	paths    []string
	skills   map[string]Skill
	warnings []string
}

func NewManager(workDir string, paths ...string) *Manager {
	root, err := filepath.Abs(strings.TrimSpace(workDir))
	if err != nil || strings.TrimSpace(workDir) == "" {
		root, _ = os.Getwd()
	}
	if len(paths) == 0 {
		home, _ := os.UserHomeDir()
		paths = []string{
			filepath.Join(root, "skills"),
			filepath.Join(root, ".your-agent", "skills"),
			filepath.Join(home, ".your-agent", "skills"),
		}
	}
	return &Manager{paths: paths, skills: make(map[string]Skill)}
}

func (manager *Manager) LoadAll() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.skills = make(map[string]Skill)
	manager.warnings = nil
	for _, root := range manager.paths {
		if err := manager.loadPath(root); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (manager *Manager) loadPath(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && filepath.Dir(path) != root {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !isSkillDocument(root, path) {
			return nil
		}
		skill, err := ParseFile(path)
		if err != nil {
			manager.warnings = append(manager.warnings, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if missing := missingRequirements(skill.Requires); len(missing) > 0 {
			manager.warnings = append(manager.warnings, fmt.Sprintf("%s skipped; missing %s", skill.Name, strings.Join(missing, ", ")))
			return nil
		}
		manager.skills[skill.Name] = skill
		return nil
	})
}

func ParseFile(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	if len(data) > maxSkillBytes {
		return Skill{}, fmt.Errorf("skill exceeds %d bytes", maxSkillBytes)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Skill{}, errors.New("missing YAML frontmatter")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return Skill{}, errors.New("unterminated YAML frontmatter")
	}
	end += 4
	var header frontmatter
	if err := yaml.Unmarshal([]byte(text[4:end]), &header); err != nil {
		return Skill{}, fmt.Errorf("parse frontmatter: %w", err)
	}
	name := strings.TrimSpace(header.Name)
	if name == "" {
		return Skill{}, errors.New("skill name is required")
	}
	requires := header.Metadata.YourAgent.Requires
	if len(requires.Bins) == 0 && len(requires.Env) == 0 {
		requires = header.Metadata.Verdent.Requires
	}
	content := strings.TrimSpace(text[end+5:])
	if content == "" {
		return Skill{}, errors.New("skill content is required")
	}
	return Skill{Name: name, Description: strings.TrimSpace(header.Description), Content: content, Path: path, Requires: requires}, nil
}

func (manager *Manager) List() []Skill {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	items := make([]Skill, 0, len(manager.skills))
	for _, skill := range manager.skills {
		copy := skill
		copy.Content = ""
		items = append(items, copy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func (manager *Manager) Get(name string) (Skill, bool) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	skill, ok := manager.skills[strings.TrimSpace(name)]
	return skill, ok
}

func (manager *Manager) Warnings() []string {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return append([]string(nil), manager.warnings...)
}

func (manager *Manager) FormatForPrompt() string {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if len(manager.skills) == 0 {
		return ""
	}
	names := make([]string, 0, len(manager.skills))
	for name := range manager.skills {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	out.WriteString("\n\n<available_skills>\n")
	for _, name := range names {
		skill := manager.skills[name]
		section := fmt.Sprintf("## %s\n%s\n\n%s\n", skill.Name, skill.Description, skill.Content)
		if out.Len()+len(section) > maxPromptSkillBytes {
			out.WriteString("[additional skills omitted by prompt budget]\n")
			break
		}
		out.WriteString(section)
	}
	out.WriteString("</available_skills>")
	return out.String()
}

func isSkillDocument(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	dir := filepath.Dir(rel)
	if filepath.Base(path) == "SKILL.md" {
		return dir == "." || filepath.Dir(dir) == "."
	}
	return strings.EqualFold(filepath.Ext(path), ".md") && dir == "."
}

func missingRequirements(requires Requires) []string {
	var missing []string
	for _, bin := range requires.Bins {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, "bin:"+bin)
		}
	}
	for _, name := range requires.Env {
		if os.Getenv(name) == "" {
			missing = append(missing, "env:"+name)
		}
	}
	return missing
}
