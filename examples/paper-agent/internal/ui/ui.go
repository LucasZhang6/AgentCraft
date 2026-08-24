// Package ui provides the terminal presentation used by the Paper Agent CLI.
package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/render"
)

type LineReader interface {
	ReadLine() (string, error)
	ReadMultiline(string) (string, error)
	ReadPrompt(string) (string, error)
	Stdout() io.Writer
	Stderr() io.Writer
}

type Theme struct {
	Primary   string
	Secondary string
	Success   string
	Error     string
	Warning   string
	Info      string
	Reset     string
	Bold      string
	Dim       string
}

var DefaultTheme = Theme{
	Primary: "\033[38;5;75m", Secondary: "\033[38;5;245m",
	Success: "\033[38;5;82m", Error: "\033[38;5;196m",
	Warning: "\033[38;5;214m", Info: "\033[38;5;39m",
	Reset: "\033[0m", Bold: "\033[1m", Dim: "\033[2m",
}

type UI struct {
	Theme  Theme
	reader *bufio.Reader
	outMu  sync.RWMutex
	out    io.Writer
	err    io.Writer
	line   LineReader
	render *render.Renderer
}

func New() *UI {
	theme := DefaultTheme
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		theme = Theme{}
	}
	markdown, _ := render.New()
	return &UI{Theme: theme, reader: bufio.NewReader(os.Stdin), out: os.Stdout, err: os.Stderr, render: markdown}
}

func (u *UI) AttachReader(reader LineReader) {
	u.line = reader
	if reader != nil {
		u.SetOut(reader.Stdout())
		u.SetErr(reader.Stderr())
	}
}

func (u *UI) SetOut(writer io.Writer) {
	u.outMu.Lock()
	defer u.outMu.Unlock()
	if writer == nil {
		writer = os.Stdout
	}
	u.out = writer
}

func (u *UI) SetErr(writer io.Writer) {
	u.outMu.Lock()
	defer u.outMu.Unlock()
	if writer == nil {
		writer = os.Stderr
	}
	u.err = writer
}

func (u *UI) Out() io.Writer {
	u.outMu.RLock()
	defer u.outMu.RUnlock()
	if u.out == nil {
		return os.Stdout
	}
	return u.out
}

func (u *UI) Err() io.Writer {
	u.outMu.RLock()
	defer u.outMu.RUnlock()
	if u.err == nil {
		return os.Stderr
	}
	return u.err
}

func (u *UI) Print(format string, args ...any) {
	fmt.Fprintf(u.Out(), format, args...)
}

func (u *UI) Println(format string, args ...any) {
	fmt.Fprintf(u.Out(), format+"\n", args...)
}

func (u *UI) PrintOutput(content string) {
	content = strings.TrimSpace(content)
	if u.Theme.Reset != "" && u.render != nil {
		content = u.render.Markdown(content)
	}
	u.Print("%s\n", content)
}

func (u *UI) PrintSuccess(format string, args ...any) {
	u.Print("%s%s%s\n", u.Theme.Success, fmt.Sprintf(format, args...), u.Theme.Reset)
}

func (u *UI) PrintError(format string, args ...any) {
	fmt.Fprintf(u.Err(), "%s%s%s\n", u.Theme.Error, fmt.Sprintf(format, args...), u.Theme.Reset)
}

func (u *UI) PrintWarning(format string, args ...any) {
	u.Print("%s%s%s\n", u.Theme.Warning, fmt.Sprintf(format, args...), u.Theme.Reset)
}

func (u *UI) PrintInfo(format string, args ...any) {
	u.Print("%s%s%s\n", u.Theme.Info, fmt.Sprintf(format, args...), u.Theme.Reset)
}

func (u *UI) PrintThinking(content string) {
	if strings.TrimSpace(content) != "" {
		u.Print("%s%s[Thinking]%s %s%s%s\n", u.Theme.Dim, u.Theme.Secondary, u.Theme.Reset, u.Theme.Dim, content, u.Theme.Reset)
	}
}

func (u *UI) PrintToolCall(toolName string, args map[string]any) {
	u.Print("%s%s[Tool: %s]%s\n", u.Theme.Dim, u.Theme.Secondary, toolName, u.Theme.Reset)
	if len(args) == 0 {
		return
	}
	encoded, _ := json.Marshal(args)
	u.Print("%s  %s%s\n", u.Theme.Dim, encoded, u.Theme.Reset)
}

func (u *UI) PrintToolResult(success bool, output string) {
	output = strings.TrimSpace(output)
	if success {
		if output != "" {
			u.Print("%s%s%s\n", u.Theme.Dim, output, u.Theme.Reset)
		}
		u.Print("%sDone%s\n\n", u.Theme.Success, u.Theme.Reset)
		return
	}
	u.Print("%sFailed: %s%s\n\n", u.Theme.Error, output, u.Theme.Reset)
}

func (u *UI) PrintBanner() {
	u.Print("%s%sPaper Agent%s\n", u.Theme.Bold, u.Theme.Primary, u.Theme.Reset)
	u.Print("%sPaper reading, structured action, durable sessions.%s\n\n", u.Theme.Secondary, u.Theme.Reset)
}

func (u *UI) PrintHelp() {
	u.Println("%s%sSlash commands%s", u.Theme.Bold, u.Theme.Primary, u.Theme.Reset)
	u.Println("")
	u.Println("  %sGeneral%s", u.Theme.Bold, u.Theme.Reset)
	u.Println("  %s/help%s, %s/h%s       Show this help", u.Theme.Info, u.Theme.Reset, u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/paste%s           Enter multiline input; finish with EOF", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/status%s          Show provider, model, session and usage", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/clear%s           Clear the screen", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/exit%s, %s/quit%s   Exit", u.Theme.Info, u.Theme.Reset, u.Theme.Info, u.Theme.Reset)
	u.Println("")
	u.Println("  %sSession%s", u.Theme.Bold, u.Theme.Reset)
	u.Println("  %s/new%s                         Start a new session", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/session%s, %s/session info%s    Show current session", u.Theme.Info, u.Theme.Reset, u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/session list%s                 List recent sessions", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/session save <title>%s         Save the current title", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/session fork [title]%s         Fork and enter the copy", u.Theme.Info, u.Theme.Reset)
	u.Println("")
	u.Println("  %sMemory%s", u.Theme.Bold, u.Theme.Reset)
	u.Println("  %s/memory%s                       List long-term memories", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/memory add <text>%s            Add project memory", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/memory search <query>%s        Search with retrieval budget", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/memory forget <id>%s           Archive a memory", u.Theme.Info, u.Theme.Reset)
	u.Println("")
	u.Println("  %sModel and task%s", u.Theme.Bold, u.Theme.Reset)
	u.Println("  %s/model%s, %s/model list%s         Show the current model", u.Theme.Info, u.Theme.Reset, u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/model use <id>%s               Switch model for later turns", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/todo%s, %s/todo stats%s          Inspect the last validated plan", u.Theme.Info, u.Theme.Reset, u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/plan [show|list]%s              Inspect persisted plans", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/plan resume [id]%s              Resume incomplete persisted steps", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/plan accept <id> <step>%s        Record human acceptance", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/goal <objective>%s             Run a goal with continuation", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/goal status%s                  Show the last goal state", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/goal pause%s                   Pause the persisted goal", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/goal resume [--auto]%s         Resume the same goal", u.Theme.Info, u.Theme.Reset)
	u.Println("  %s/goal clear%s                   Clear only the persisted goal", u.Theme.Info, u.Theme.Reset)
	u.Println("")
}

func (u *UI) Prompt() (string, error) {
	if u.line != nil {
		return u.line.ReadLine()
	}
	u.Print("%s%s> %s", u.Theme.Bold, u.Theme.Primary, u.Theme.Reset)
	line, err := u.reader.ReadString('\n')
	return strings.TrimSpace(line), err
}

func (u *UI) PromptMultiline() (string, error) {
	u.PrintInfo("多行输入模式：单独输入 EOF 结束。")
	if u.line != nil {
		return u.line.ReadMultiline("EOF")
	}
	var lines []string
	for {
		line, err := u.reader.ReadString('\n')
		if strings.TrimSpace(line) == "EOF" {
			return strings.TrimSpace(strings.Join(lines, "")), nil
		}
		if line != "" {
			lines = append(lines, line)
		}
		if err != nil {
			return strings.TrimSpace(strings.Join(lines, "")), err
		}
	}
}

func (u *UI) PromptApproval(toolName, risk string, args map[string]any) bool {
	u.PrintWarning("Tool execution requires %s approval:", risk)
	u.PrintToolCall(toolName, args)
	if u.line != nil {
		answer, _ := u.line.ReadPrompt("Allow this operation? [y/N] ")
		answer = strings.ToLower(strings.TrimSpace(answer))
		return answer == "y" || answer == "yes"
	}
	u.Print("Allow this operation? [y/N] ")
	line, _ := u.reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func (u *UI) PromptQuestion(question string) (string, error) {
	if u.line != nil {
		return u.line.ReadPrompt(strings.TrimSpace(question) + "\n> ")
	}
	u.Print("%s%s%s\n%s> %s", u.Theme.Info, strings.TrimSpace(question), u.Theme.Reset, u.Theme.Bold, u.Theme.Reset)
	line, err := u.reader.ReadString('\n')
	return strings.TrimSpace(line), err
}

func (u *UI) Spinner(message string) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(stopped)
		chars := []string{"|", "/", "-", "\\"}
		width := displayWidth(message) + 4
		for index := 0; ; index++ {
			select {
			case <-done:
				u.Print("\r%s\r", strings.Repeat(" ", width))
				return
			default:
				u.Print("\r%s %s%s%s", message, u.Theme.Primary, chars[index%len(chars)], u.Theme.Reset)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return func() {
		once.Do(func() {
			close(done)
			<-stopped
		})
	}
}

func displayWidth(value string) int {
	width := 0
	for _, current := range value {
		if unicode.Is(unicode.Han, current) || unicode.Is(unicode.Hangul, current) ||
			unicode.Is(unicode.Hiragana, current) || unicode.Is(unicode.Katakana, current) {
			width += 2
			continue
		}
		width++
	}
	return width
}
