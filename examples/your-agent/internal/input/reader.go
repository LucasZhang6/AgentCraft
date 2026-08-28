package input

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ergochat/readline"
)

type Reader struct{ line *readline.Instance }

func New(historyPath string) (*Reader, error) {
	if strings.TrimSpace(historyPath) == "" {
		historyPath = filepath.Join(".agent-data", "history")
	}
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o700); err != nil {
		return nil, fmt.Errorf("create history directory: %w", err)
	}
	completer := readline.NewPrefixCompleter(
		readline.PcItem("/help"), readline.PcItem("/status"), readline.PcItem("/clear"), readline.PcItem("/exit"),
		readline.PcItem("/session", readline.PcItem("list"), readline.PcItem("info"), readline.PcItem("save"), readline.PcItem("fork")),
		readline.PcItem("/memory", readline.PcItem("add"), readline.PcItem("search"), readline.PcItem("forget")),
		readline.PcItem("/goal", readline.PcItem("status"), readline.PcItem("pause"), readline.PcItem("resume"), readline.PcItem("clear")),
		readline.PcItem("/plan", readline.PcItem("show"), readline.PcItem("list"), readline.PcItem("resume"), readline.PcItem("accept")),
		readline.PcItem("/model", readline.PcItem("list"), readline.PcItem("use")),
	)
	instance, err := readline.NewEx(&readline.Config{
		Prompt:            "\033[1m\033[38;5;75m> \033[0m",
		HistoryFile:       historyPath,
		HistoryLimit:      2000,
		HistorySearchFold: true,
		AutoComplete:      completer,
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
	})
	if err != nil {
		return nil, err
	}
	return &Reader{line: instance}, nil
}

func (reader *Reader) ReadLine() (string, error) {
	line, err := reader.line.Readline()
	return strings.TrimSpace(line), err
}

func (reader *Reader) ReadMultiline(sentinel string) (string, error) {
	if sentinel == "" {
		sentinel = "EOF"
	}
	old := reader.line.GetConfig().Prompt
	reader.line.SetPrompt("... ")
	defer reader.line.SetPrompt(old)
	var lines []string
	for {
		line, err := reader.line.Readline()
		if err != nil {
			if err == io.EOF {
				return strings.Join(lines, "\n"), nil
			}
			return "", err
		}
		if strings.TrimSpace(line) == sentinel {
			return strings.Join(lines, "\n"), nil
		}
		lines = append(lines, line)
	}
}

func (reader *Reader) ReadPrompt(prompt string) (string, error) {
	old := reader.line.GetConfig().Prompt
	reader.line.SetPrompt(prompt)
	defer reader.line.SetPrompt(old)
	line, err := reader.line.Readline()
	return strings.TrimSpace(line), err
}

func (reader *Reader) Stdout() io.Writer { return reader.line.Stdout() }
func (reader *Reader) Stderr() io.Writer { return reader.line.Stderr() }
func (reader *Reader) Close() error      { return reader.line.Close() }
