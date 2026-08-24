package evaluator

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
)

var sourcePattern = regexp.MustCompile(`https?://`)

type ReportEvaluator struct{}

func (ReportEvaluator) Evaluate(report string) domain.Evaluation {
	checks := map[string]bool{
		"enoughDetail":           utf8.RuneCountInString(report) >= 300,
		"explainsProblem":        strings.Contains(report, "问题背景"),
		"explainsMethod":         strings.Contains(report, "核心方法"),
		"givesEngineeringAdvice": strings.Contains(report, "工程启发"),
		"statesLimitations":      strings.Contains(report, "局限"),
		"citesSource":            sourcePattern.MatchString(report),
	}
	passed := 0
	for _, value := range checks {
		if value {
			passed++
		}
	}
	return domain.Evaluation{
		Passed: passed == len(checks),
		Score:  float64(passed) / float64(len(checks)),
		Checks: checks,
	}
}
