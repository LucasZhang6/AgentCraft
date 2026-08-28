package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

const (
	semanticMaxFiles      = 5000
	semanticMaxFileBytes  = 1024 * 1024
	semanticDefaultResult = 8
)

var semanticTokenPattern = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

var symbolPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)`),
	regexp.MustCompile(`^\s*(?:type|class|interface|struct|enum)\s+([A-Za-z_][A-Za-z0-9_]*)`),
	regexp.MustCompile(`^\s*(?:def|fn|function)\s+([A-Za-z_][A-Za-z0-9_]*)`),
	regexp.MustCompile(`^\s*(?:const|var|let)\s+([A-Za-z_][A-Za-z0-9_]*)`),
}

type semanticSymbol struct {
	Name string
	Line int
}

type semanticFile struct {
	Path    string
	Lines   []string
	Symbols []semanticSymbol
}

type semanticHit struct {
	File   semanticFile
	Score  int
	Line   int
	Reason string
}

func semanticCodeSearchDefinition(cwd string) Definition {
	return Definition{Name: "semantic_code_search", Description: "Search code by symbol definition, lexical references, or ranked concept terms using a local workspace symbol index.", Risk: domain.RiskRead,
		SupportsParallel: true,
		Schema: domain.ToolSchema{Type: "object", Required: []string{"query"}, Properties: map[string]domain.ToolField{
			"query": {Type: "string"}, "action": {Type: "string"}, "path": {Type: "string"}, "max_results": {Type: "number"},
		}}, MaxOutputBytes: 48 * 1024, Execute: func(ctx context.Context, args map[string]any) (any, error) {
			query := strings.TrimSpace(args["query"].(string))
			if query == "" {
				return nil, errors.New("semantic query cannot be empty")
			}
			action, _ := args["action"].(string)
			action = strings.ToLower(strings.TrimSpace(action))
			if action == "" {
				action = "search"
			}
			if action != "search" && action != "definition" && action != "references" {
				return nil, errors.New("semantic action must be search, definition, or references")
			}
			rootValue, _ := args["path"].(string)
			root, err := resolveWorkspacePath(cwd, rootValue, false)
			if err != nil {
				return nil, err
			}
			maxResults := numberArg(args["max_results"], semanticDefaultResult, 1, 30)
			files, err := buildSemanticIndex(ctx, cwd, root)
			if err != nil {
				return nil, err
			}
			hits := rankSemanticFiles(files, query, action)
			if len(hits) > maxResults {
				hits = hits[:maxResults]
			}
			return renderSemanticHits(query, action, hits, len(files)), nil
		}}
}

func buildSemanticIndex(ctx context.Context, cwd, root string) ([]semanticFile, error) {
	var files []semanticFile
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".gocache", "node_modules", "vendor", "dist", "build", ".next", "coverage":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !semanticCodeExtension(filepath.Ext(path)) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > semanticMaxFileBytes {
			return nil
		}
		file, err := indexSemanticFile(ctx, cwd, path)
		if err != nil {
			return err
		}
		files = append(files, file)
		if len(files) >= semanticMaxFiles {
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return files, nil
}

func indexSemanticFile(ctx context.Context, cwd, path string) (semanticFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return semanticFile{}, err
	}
	defer file.Close()
	item := semanticFile{Path: workspaceRelative(cwd, path)}
	scanner := bufio.NewScanner(io.LimitReader(file, semanticMaxFileBytes))
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return semanticFile{}, err
		}
		line := scanner.Text()
		item.Lines = append(item.Lines, line)
		for _, pattern := range symbolPatterns {
			match := pattern.FindStringSubmatch(line)
			if len(match) == 2 {
				item.Symbols = append(item.Symbols, semanticSymbol{Name: match[1], Line: len(item.Lines)})
				break
			}
		}
	}
	return item, scanner.Err()
}

func rankSemanticFiles(files []semanticFile, query, action string) []semanticHit {
	terms := semanticTerms(query)
	lowerQuery := strings.ToLower(query)
	var hits []semanticHit
	for _, file := range files {
		hit := semanticHit{File: file}
		pathLower := strings.ToLower(file.Path)
		for _, symbol := range file.Symbols {
			name := strings.ToLower(symbol.Name)
			score := 0
			if name == lowerQuery {
				score += 180
			} else if strings.Contains(name, lowerQuery) {
				score += 100
			}
			for _, term := range terms {
				if strings.Contains(name, term) {
					score += 35
				}
			}
			if action == "definition" {
				score *= 2
			}
			if score > hit.Score {
				hit.Score, hit.Line, hit.Reason = score, symbol.Line, "symbol:"+symbol.Name
			}
		}
		for lineIndex, line := range file.Lines {
			lower := strings.ToLower(line)
			lineScore := 0
			if strings.Contains(lower, lowerQuery) {
				lineScore += 80
			}
			for _, term := range terms {
				if strings.Contains(lower, term) {
					lineScore += 12
				}
				if strings.Contains(pathLower, term) {
					lineScore += 4
				}
			}
			if action == "references" && lineScore > 0 {
				lineScore += 30
			}
			if lineScore > hit.Score {
				hit.Score, hit.Line, hit.Reason = lineScore, lineIndex+1, "ranked reference"
			}
		}
		if hit.Score > 0 {
			hits = append(hits, hit)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].File.Path < hits[j].File.Path
		}
		return hits[i].Score > hits[j].Score
	})
	return hits
}

func renderSemanticHits(query, action string, hits []semanticHit, indexed int) string {
	var out strings.Builder
	fmt.Fprintf(&out, "semantic_code_search\nmode: local-symbol-index\naction: %s\nquery: %s\nindexed_files: %d\n", action, query, indexed)
	if len(hits) == 0 {
		out.WriteString("no matching symbols or references found\n")
		return out.String()
	}
	for index, hit := range hits {
		start, end := max(1, hit.Line-2), min(len(hit.File.Lines), hit.Line+2)
		fmt.Fprintf(&out, "\n%d. %s:%d score=%d reason=%s range=%d-%d\n", index+1, hit.File.Path, hit.Line, hit.Score, hit.Reason, start, end)
		for line := start; line <= end; line++ {
			fmt.Fprintf(&out, "%d|%s\n", line, hit.File.Lines[line-1])
		}
	}
	return out.String()
}

func semanticTerms(query string) []string {
	seen := map[string]struct{}{}
	var terms []string
	for _, value := range semanticTokenPattern.FindAllString(strings.ToLower(query), -1) {
		if len(value) < 2 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		terms = append(terms, value)
	}
	return terms
}

func semanticCodeExtension(extension string) bool {
	switch strings.ToLower(extension) {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".py", ".rs", ".java", ".kt", ".kts", ".c", ".cc", ".cpp", ".h", ".hpp", ".rb", ".php", ".swift":
		return true
	default:
		return false
	}
}

func numberArg(value any, fallback, minimum, maximum int) int {
	number, ok := value.(float64)
	if !ok {
		return fallback
	}
	result := int(number)
	if result < minimum || result > maximum {
		return fallback
	}
	return result
}
