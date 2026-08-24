package ui

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestUIUsesInjectedWritersWithoutColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	var errors bytes.Buffer
	terminal := New()
	terminal.SetOut(&output)
	terminal.SetErr(&errors)

	terminal.PrintInfo("session=%s", "session_1")
	terminal.PrintError("failed: %s", "boom")
	if got := output.String(); got != "session=session_1\n" {
		t.Fatalf("output = %q", got)
	}
	if got := errors.String(); got != "failed: boom\n" {
		t.Fatalf("errors = %q", got)
	}
}

func TestPrintToolCallRendersStructuredArgs(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	terminal := New()
	terminal.SetOut(&output)
	terminal.PrintToolCall("read_paper_card", map[string]any{"paper_id": "react"})

	if !strings.Contains(output.String(), "[Tool: read_paper_card]") ||
		!strings.Contains(output.String(), `"paper_id":"react"`) {
		t.Fatalf("tool output = %q", output.String())
	}
}

func TestDisplayWidthCountsCJKAsDoubleWidth(t *testing.T) {
	if got := displayWidth("Agent 处理"); got != 10 {
		t.Fatalf("display width = %d, want 10", got)
	}
}

func TestPromptMultilineStopsAtEOFLine(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var output bytes.Buffer
	terminal := New()
	terminal.SetOut(&output)
	terminal.reader = bufio.NewReader(strings.NewReader("first line\nsecond line\nEOF\n"))
	value, err := terminal.PromptMultiline()
	if err != nil {
		t.Fatalf("prompt multiline: %v", err)
	}
	if value != "first line\nsecond line" {
		t.Fatalf("multiline value = %q", value)
	}
}
