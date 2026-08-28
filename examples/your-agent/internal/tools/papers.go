package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LucasZhang6/AgentCraft/examples/your-agent/internal/domain"
)

func LoadCatalog(data []byte) ([]domain.Paper, error) {
	var papers []domain.Paper
	if err := json.Unmarshal(data, &papers); err != nil {
		return nil, fmt.Errorf("decode paper catalog: %w", err)
	}
	if len(papers) == 0 {
		return nil, errors.New("paper catalog must be a non-empty array")
	}
	return papers, nil
}

func RegisterPaperTools(registry *Registry, papers []domain.Paper) error {
	if err := registry.Register(Definition{
		Name: "search_papers", Description: "Search the local, curated paper catalog by topic.", Risk: "read-only",
		SupportsParallel: true, ConcurrencyGroup: "paper-catalog",
		Schema: domain.ToolSchema{
			Type:       "object",
			Required:   []string{"query"},
			Properties: map[string]domain.ToolField{"query": {Type: "string"}},
		},
		Execute: func(_ context.Context, args map[string]any) (any, error) {
			query := args["query"].(string)
			type rankedPaper struct {
				paper domain.Paper
				score int
			}
			ranked := make([]rankedPaper, 0, len(papers))
			for _, paper := range papers {
				ranked = append(ranked, rankedPaper{paper: paper, score: scorePaper(paper, query)})
			}
			sort.SliceStable(ranked, func(i, j int) bool {
				if ranked[i].score != ranked[j].score {
					return ranked[i].score > ranked[j].score
				}
				return ranked[i].paper.Title < ranked[j].paper.Title
			})

			matches := make([]rankedPaper, 0, len(ranked))
			for _, item := range ranked {
				if item.score > 0 {
					matches = append(matches, item)
				}
			}
			if len(matches) == 0 {
				matches = ranked
			}
			if len(matches) > 3 {
				matches = matches[:3]
			}
			result := make([]domain.PaperMatch, 0, len(matches))
			for _, item := range matches {
				result = append(result, domain.PaperMatch{
					ID: item.paper.ID, Title: item.paper.Title, Module: item.paper.Module, Score: item.score,
				})
			}
			return result, nil
		},
	}); err != nil {
		return err
	}

	return registry.Register(Definition{
		Name: "read_paper_card", Description: "Read a curated structured card from the local paper catalog.", Risk: "read-only",
		SupportsParallel: true, ConcurrencyGroup: "paper-catalog",
		Schema: domain.ToolSchema{
			Type:       "object",
			Required:   []string{"id"},
			Properties: map[string]domain.ToolField{"id": {Type: "string"}},
		},
		Execute: func(_ context.Context, args map[string]any) (any, error) {
			id := strings.TrimSpace(args["id"].(string))
			normalized := normalizePaperLookup(id)
			for _, paper := range papers {
				if paper.ID == id || normalizePaperLookup(paper.ID) == normalized || normalizePaperLookup(paper.Title) == normalized {
					return paper, nil
				}
			}
			return nil, fmt.Errorf("unknown paper id: %s", id)
		},
	})
}

func normalizePaperLookup(value string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			return unicode.ToLower(char)
		}
		return -1
	}, value)
}

func scorePaper(paper domain.Paper, query string) int {
	parts := append([]string{paper.Title, paper.Module}, paper.Keywords...)
	source := strings.ToLower(strings.Join(parts, " "))
	terms := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",，。:：/", r)
	})
	score := 0
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if utf8.RuneCountInString(term) >= 2 && strings.Contains(source, term) {
			score++
		}
	}
	return score
}
