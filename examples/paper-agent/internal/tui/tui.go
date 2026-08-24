package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/render"
)

type Handler func(context.Context, string, func(string)) (string, error)

type message struct{ role, content string }
type deltaMessage string
type completedMessage struct{ content string }
type failedMessage struct{ err error }

type bridge struct{ program *tea.Program }

type Model struct {
	context   context.Context
	sessionID string
	modelID   string
	handler   Handler
	bridge    *bridge
	viewport  viewport.Model
	input     textinput.Model
	messages  []message
	renderer  *render.Renderer
	status    string
	width     int
	height    int
	ready     bool
	running   bool
}

func Run(ctx context.Context, sessionID, modelID string, handler Handler) error {
	input := textinput.New()
	input.Placeholder = "Ask or direct the agent"
	input.CharLimit = 16 * 1024
	input.Focus()
	renderer, _ := render.New()
	shared := &bridge{}
	model := &Model{
		context: ctx, sessionID: sessionID, modelID: modelID, handler: handler, bridge: shared,
		viewport: viewport.New(80, 20), input: input, renderer: renderer, status: "ready",
	}
	program := tea.NewProgram(model, tea.WithAltScreen())
	shared.program = program
	_, err := program.Run()
	return err
}

func (model *Model) Init() tea.Cmd { return textinput.Blink }

func (model *Model) Update(value tea.Msg) (tea.Model, tea.Cmd) {
	switch value := value.(type) {
	case tea.KeyMsg:
		switch value.String() {
		case "ctrl+c":
			return model, tea.Quit
		case "enter":
			input := strings.TrimSpace(model.input.Value())
			if input == "" || model.running {
				return model, nil
			}
			if input == "/exit" || input == "/quit" {
				return model, tea.Quit
			}
			model.messages = append(model.messages, message{role: "user", content: input}, message{role: "assistant"})
			model.input.SetValue("")
			model.running = true
			model.status = "running"
			model.refresh()
			go func() {
				content, err := model.handler(model.context, input, func(delta string) {
					model.bridge.program.Send(deltaMessage(delta))
				})
				if err != nil {
					model.bridge.program.Send(failedMessage{err: err})
					return
				}
				model.bridge.program.Send(completedMessage{content: content})
			}()
			return model, nil
		}
	case tea.WindowSizeMsg:
		model.width, model.height, model.ready = value.Width, value.Height, true
		model.viewport.Width = max(value.Width-2, 20)
		model.viewport.Height = max(value.Height-7, 5)
		model.input.Width = max(value.Width-6, 20)
		model.refresh()
	case deltaMessage:
		if len(model.messages) > 0 {
			model.messages[len(model.messages)-1].content += string(value)
			model.refresh()
		}
	case completedMessage:
		if len(model.messages) > 0 && model.messages[len(model.messages)-1].content == "" {
			model.messages[len(model.messages)-1].content = value.content
		}
		model.running = false
		model.status = "ready"
		model.refresh()
	case failedMessage:
		model.messages = append(model.messages, message{role: "error", content: value.err.Error()})
		model.running = false
		model.status = "failed"
		model.refresh()
	}
	var inputCommand, viewportCommand tea.Cmd
	model.input, inputCommand = model.input.Update(value)
	model.viewport, viewportCommand = model.viewport.Update(value)
	return model, tea.Batch(inputCommand, viewportCommand)
}

func (model *Model) View() string {
	if !model.ready {
		return "Initializing Paper Agent..."
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Render("Paper Agent")
	status := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(
		fmt.Sprintf("session %s | model %s | %s", model.sessionID, model.modelID, model.status),
	)
	input := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("245")).Padding(0, 1).Render(model.input.View())
	help := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("Enter send | Ctrl+C exit")
	return lipgloss.JoinVertical(lipgloss.Left, title, status, model.viewport.View(), input, help)
}

func (model *Model) refresh() {
	var output strings.Builder
	for _, item := range model.messages {
		switch item.role {
		case "user":
			output.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42")).Render("You"))
		case "assistant":
			output.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Render("Agent"))
		case "error":
			output.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("Error"))
		}
		output.WriteString("\n")
		content := item.content
		if item.role == "assistant" && model.renderer != nil && !model.running {
			content = model.renderer.Markdown(content)
		}
		output.WriteString(content)
		output.WriteString("\n\n")
	}
	model.viewport.SetContent(output.String())
	model.viewport.GotoBottom()
}
