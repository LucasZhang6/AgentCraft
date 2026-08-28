package render

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

type Renderer struct{ markdown *glamour.TermRenderer }

func New() (*Renderer, error) {
	markdown, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(100))
	if err != nil {
		return nil, err
	}
	return &Renderer{markdown: markdown}, nil
}

func (renderer *Renderer) Markdown(value string) string {
	if renderer == nil || renderer.markdown == nil || strings.TrimSpace(value) == "" {
		return value
	}
	output, err := renderer.markdown.Render(value)
	if err != nil {
		return value
	}
	return strings.TrimRight(output, "\n")
}
