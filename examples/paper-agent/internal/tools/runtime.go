package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
)

type ClarifyFunc func(context.Context, string) (string, error)
type SubAgentFunc func(context.Context, string) (string, error)

type RuntimeOptions struct {
	WorkDir      string
	HTTPClient   *http.Client
	WebSearchKey string
	Clarify      ClarifyFunc
	SubAgent     SubAgentFunc
}

func RegisterRuntimeTools(registry *Registry, options RuntimeOptions) error {
	if registry == nil {
		return errors.New("runtime tool registry is required")
	}
	cwd, err := filepath.Abs(strings.TrimSpace(options.WorkDir))
	if err != nil || strings.TrimSpace(options.WorkDir) == "" {
		cwd, err = os.Getwd()
	}
	if err != nil {
		return fmt.Errorf("resolve tool working directory: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(cwd); resolveErr == nil {
		cwd = resolved
	}
	client := options.HTTPClient
	if client == nil {
		client = safeHTTPClient(30 * time.Second)
	}
	definitions := []Definition{
		fileReadDefinition(cwd), fileWriteDefinition(cwd), fileEditDefinition(cwd),
		listDirDefinition(cwd), globDefinition(cwd), grepDefinition(cwd), bashDefinition(cwd),
		webFetchDefinition(client), webSearchDefinition(client, options.WebSearchKey),
		clarificationDefinition(options.Clarify), subAgentDefinition(options.SubAgent),
	}
	for _, definition := range definitions {
		if err := registry.Register(definition); err != nil {
			return err
		}
	}
	return nil
}

func stringSchema(required []string, fields ...string) domain.ToolSchema {
	properties := make(map[string]domain.ToolField, len(fields))
	for _, field := range fields {
		properties[field] = domain.ToolField{Type: "string"}
	}
	return domain.ToolSchema{Type: "object", Required: required, Properties: properties}
}

func fileReadDefinition(cwd string) Definition {
	return Definition{Name: "file_read", Description: "Read a UTF-8 text file inside the workspace.", Risk: domain.RiskRead,
		Schema: stringSchema([]string{"path"}, "path"), Execute: func(_ context.Context, args map[string]any) (any, error) {
			path, err := resolveWorkspacePath(cwd, args["path"].(string), false)
			if err != nil {
				return nil, err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			return string(data), nil
		}}
}

func fileWriteDefinition(cwd string) Definition {
	return Definition{Name: "file_write", Description: "Write a text file inside the workspace, creating parent directories.", Risk: domain.RiskWrite,
		Schema: stringSchema([]string{"path", "content"}, "path", "content"), Execute: func(_ context.Context, args map[string]any) (any, error) {
			path, err := resolveWorkspacePath(cwd, args["path"].(string), true)
			if err != nil {
				return nil, err
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, []byte(args["content"].(string)), 0o644); err != nil {
				return nil, err
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(args["content"].(string)), workspaceRelative(cwd, path)), nil
		}}
}

func fileEditDefinition(cwd string) Definition {
	return Definition{Name: "file_edit", Description: "Replace one exact text occurrence in a workspace file.", Risk: domain.RiskWrite,
		Schema: stringSchema([]string{"path", "old_text", "new_text"}, "path", "old_text", "new_text"), Execute: func(_ context.Context, args map[string]any) (any, error) {
			path, err := resolveWorkspacePath(cwd, args["path"].(string), false)
			if err != nil {
				return nil, err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			oldText, newText := args["old_text"].(string), args["new_text"].(string)
			if oldText == "" {
				return nil, errors.New("old_text cannot be empty")
			}
			if bytes.Count(data, []byte(oldText)) != 1 {
				return nil, fmt.Errorf("old_text must occur exactly once")
			}
			updated := bytes.Replace(data, []byte(oldText), []byte(newText), 1)
			if err := os.WriteFile(path, updated, 0o644); err != nil {
				return nil, err
			}
			return "updated " + workspaceRelative(cwd, path), nil
		}}
}

func listDirDefinition(cwd string) Definition {
	return Definition{Name: "list_dir", Description: "List one workspace directory.", Risk: domain.RiskRead,
		Schema: stringSchema(nil, "path"), Execute: func(_ context.Context, args map[string]any) (any, error) {
			value, _ := args["path"].(string)
			path, err := resolveWorkspacePath(cwd, value, false)
			if err != nil {
				return nil, err
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			result := make([]string, 0, len(entries))
			for _, entry := range entries {
				suffix := ""
				if entry.IsDir() {
					suffix = "/"
				}
				result = append(result, entry.Name()+suffix)
			}
			return strings.Join(result, "\n"), nil
		}}
}

func globDefinition(cwd string) Definition {
	return Definition{Name: "glob", Description: "Match workspace paths with a filepath glob pattern.", Risk: domain.RiskRead,
		Schema: stringSchema([]string{"pattern"}, "pattern"), Execute: func(_ context.Context, args map[string]any) (any, error) {
			pattern := filepath.Clean(args["pattern"].(string))
			if filepath.IsAbs(pattern) || pattern == ".." || strings.HasPrefix(pattern, ".."+string(filepath.Separator)) {
				return nil, errors.New("glob pattern escapes workspace")
			}
			matches, err := filepath.Glob(filepath.Join(cwd, pattern))
			if err != nil {
				return nil, err
			}
			for index := range matches {
				matches[index] = workspaceRelative(cwd, matches[index])
			}
			sort.Strings(matches)
			return strings.Join(matches, "\n"), nil
		}}
}

func grepDefinition(cwd string) Definition {
	return Definition{Name: "grep", Description: "Search text files in the workspace with a regular expression.", Risk: domain.RiskRead,
		Schema: stringSchema([]string{"query"}, "query", "path"), Execute: func(ctx context.Context, args map[string]any) (any, error) {
			pattern, err := regexp.Compile(args["query"].(string))
			if err != nil {
				return nil, err
			}
			root, _ := args["path"].(string)
			path, err := resolveWorkspacePath(cwd, root, false)
			if err != nil {
				return nil, err
			}
			var matches []string
			err = filepath.WalkDir(path, func(itemPath string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == ".gocache") {
					return filepath.SkipDir
				}
				if entry.IsDir() {
					return nil
				}
				data, err := os.ReadFile(itemPath)
				if err != nil || bytes.IndexByte(data, 0) >= 0 {
					return nil
				}
				for lineNumber, line := range strings.Split(string(data), "\n") {
					if pattern.MatchString(line) {
						matches = append(matches, fmt.Sprintf("%s:%d:%s", workspaceRelative(cwd, itemPath), lineNumber+1, line))
						if len(matches) >= 500 {
							return io.EOF
						}
					}
				}
				return nil
			})
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			return strings.Join(matches, "\n"), nil
		}}
}

func bashDefinition(cwd string) Definition {
	return Definition{Name: "bash", Description: "Run a shell command in the workspace.", Risk: domain.RiskDangerous,
		Schema: stringSchema([]string{"command"}, "command"), Timeout: 10 * time.Minute, Execute: func(ctx context.Context, args map[string]any) (any, error) {
			binary, commandArgs := shellCommand(args["command"].(string))
			cmd := exec.CommandContext(ctx, binary, commandArgs...)
			cmd.Dir = cwd
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			err := cmd.Run()
			if err != nil {
				return output.String(), fmt.Errorf("command failed: %w", err)
			}
			return output.String(), nil
		}}
}

func webFetchDefinition(client *http.Client) Definition {
	return Definition{Name: "web_fetch", Description: "Fetch a public HTTP(S) page with SSRF and response-size guards.", Risk: domain.RiskRead,
		Schema: stringSchema([]string{"url"}, "url"), Execute: func(ctx context.Context, args map[string]any) (any, error) {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, args["url"].(string), nil)
			if err != nil {
				return nil, err
			}
			if err := validatePublicURL(request.URL); err != nil {
				return nil, err
			}
			request.Header.Set("User-Agent", "paper-agent/1.0")
			response, err := client.Do(request)
			if err != nil {
				return nil, err
			}
			defer response.Body.Close()
			data, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
			if err != nil {
				return nil, err
			}
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, string(data))
			}
			content := string(data)
			if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "html") {
				content = stripHTML(content)
			}
			return content, nil
		}}
}

func webSearchDefinition(client *http.Client, configuredKey string) Definition {
	return Definition{Name: "web_search", Description: "Search the public web through the Brave Search API.", Risk: domain.RiskRead,
		Schema: stringSchema([]string{"query"}, "query"), Execute: func(ctx context.Context, args map[string]any) (any, error) {
			key := strings.TrimSpace(configuredKey)
			if key == "" {
				key = strings.TrimSpace(os.Getenv("BRAVE_API_KEY"))
			}
			if key == "" {
				return nil, errors.New("web_search requires BRAVE_API_KEY")
			}
			endpoint := "https://api.search.brave.com/res/v1/web/search?q=" + url.QueryEscape(args["query"].(string)) + "&count=10"
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
			request.Header.Set("Accept", "application/json")
			request.Header.Set("X-Subscription-Token", key)
			response, err := client.Do(request)
			if err != nil {
				return nil, err
			}
			defer response.Body.Close()
			data, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
			if err != nil {
				return nil, err
			}
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return nil, fmt.Errorf("search HTTP %d: %s", response.StatusCode, string(data))
			}
			var payload struct {
				Web struct {
					Results []struct{ Title, URL, Description string } `json:"results"`
				} `json:"web"`
			}
			if err := json.Unmarshal(data, &payload); err != nil {
				return nil, err
			}
			var lines []string
			for _, item := range payload.Web.Results {
				lines = append(lines, fmt.Sprintf("- %s\n  %s\n  %s", item.Title, item.URL, item.Description))
			}
			return strings.Join(lines, "\n"), nil
		}}
}

func clarificationDefinition(clarify ClarifyFunc) Definition {
	return Definition{Name: "clarification", Description: "Ask the user for missing information before continuing.", Risk: domain.RiskRead,
		Schema: stringSchema([]string{"question"}, "question"), Execute: func(ctx context.Context, args map[string]any) (any, error) {
			if clarify == nil {
				return nil, errors.New("clarification is unavailable in this client")
			}
			return clarify(ctx, args["question"].(string))
		}}
}

func subAgentDefinition(run SubAgentFunc) Definition {
	return Definition{Name: "subagent", Description: "Delegate a bounded independent research task to a child model call.", Risk: domain.RiskRead,
		Schema: stringSchema([]string{"task"}, "task"), Timeout: 10 * time.Minute, Execute: func(ctx context.Context, args map[string]any) (any, error) {
			if run == nil {
				return nil, errors.New("subagent is unavailable for this provider")
			}
			return run(ctx, args["task"].(string))
		}}
}

func resolveWorkspacePath(cwd, value string, allowMissing bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "."
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes workspace")
	}
	check := path
	if allowMissing {
		for {
			if _, err := os.Lstat(check); err == nil {
				break
			}
			parent := filepath.Dir(check)
			if parent == check {
				return "", errors.New("cannot resolve path parent")
			}
			check = parent
		}
	}
	resolved, err := filepath.EvalSymlinks(check)
	if err != nil {
		return "", err
	}
	rel, err = filepath.Rel(cwd, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path resolves outside workspace")
	}
	return path, nil
}

func workspaceRelative(cwd, path string) string {
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func safeHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	client := &http.Client{Transport: transport, Timeout: timeout}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return validatePublicURL(request.URL)
	}
	return client
}

func validatePublicURL(target *url.URL) error {
	if target == nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" {
		return errors.New("only public HTTP(S) URLs are allowed")
	}
	addresses, err := net.LookupIP(target.Hostname())
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsUnspecified() || address.IsMulticast() {
			return fmt.Errorf("URL resolves to non-public address %s", address)
		}
	}
	return nil
}

var htmlTags = regexp.MustCompile(`(?s)<[^>]*>`)
var htmlSpace = regexp.MustCompile(`\s+`)

func stripHTML(value string) string {
	value = htmlTags.ReplaceAllString(value, " ")
	return strings.TrimSpace(htmlSpace.ReplaceAllString(value, " "))
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/D", "/S", "/C", command}
	}
	if value := strings.TrimSpace(os.Getenv("SHELL")); value != "" {
		return value, []string{"-lc", command}
	}
	return "/bin/sh", []string{"-lc", command}
}
