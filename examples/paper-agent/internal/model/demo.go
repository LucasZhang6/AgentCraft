package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LucasZhang6/AgentCraft/examples/paper-agent/internal/domain"
)

type DemoModel struct{}

func (DemoModel) CreatePlan(_ context.Context, _ string, _ []domain.ToolDescription) (domain.PlanResponse, error) {
	return domain.PlanResponse{Plan: []domain.PlanStep{
		{
			ID: "find-paper", Description: "从可信目录检索与目标相关的代表论文",
			Dependencies: []string{}, Tool: "search_papers", SuccessCriteria: "至少找到一篇论文", Status: "pending",
		},
		{
			ID: "read-card", Description: "读取论文的结构化研究卡片",
			Dependencies: []string{"find-paper"}, Tool: "read_paper_card", SuccessCriteria: "获得问题、方法、贡献、局限和来源", Status: "pending",
		},
		{
			ID: "explain", Description: "生成结构化分析并通过质量评估",
			Dependencies: []string{"read-card"}, SuccessCriteria: "包含背景、方法、工程启发、局限和原文链接", Status: "pending",
		},
	}}, nil
}

func (DemoModel) Decide(_ context.Context, input domain.DecisionContext) (domain.DecisionResponse, error) {
	search, found := findObservation(input.Observations, "search_papers")
	if !found {
		return domain.DecisionResponse{Decision: domain.Decision{
			Type: domain.DecisionTool, Tool: "search_papers", Args: map[string]any{"query": latestUserRequest(input.Goal)},
		}}, nil
	}

	matches, ok := search.Result.([]domain.PaperMatch)
	if !ok || len(matches) == 0 {
		return domain.DecisionResponse{}, errors.New("search_papers returned no typed results")
	}
	paperCard, found := findObservation(input.Observations, "read_paper_card")
	if !found {
		return domain.DecisionResponse{Decision: domain.Decision{
			Type: domain.DecisionTool, Tool: "read_paper_card", Args: map[string]any{"id": matches[0].ID},
		}}, nil
	}

	paper, ok := paperCard.Result.(domain.Paper)
	if !ok {
		return domain.DecisionResponse{}, errors.New("read_paper_card returned an invalid paper")
	}
	style := "清晰、低术语密度"
	for _, item := range input.Memories {
		if item.Key == "explanation_style" {
			style = item.Value
			break
		}
	}
	return domain.DecisionResponse{Decision: domain.Decision{
		Type: domain.DecisionFinal, Content: reportFor(paper, style), Paper: &paper,
	}}, nil
}

func latestUserRequest(goal string) string {
	const marker = "\nUser:\n"
	if index := strings.LastIndex(goal, marker); index >= 0 {
		if current := strings.TrimSpace(goal[index+len(marker):]); current != "" {
			return current
		}
	}
	return strings.TrimSpace(goal)
}

func findObservation(observations []domain.Observation, tool string) (domain.Observation, bool) {
	for index := len(observations) - 1; index >= 0; index-- {
		if observations[index].Tool == tool && observations[index].OK {
			return observations[index], true
		}
	}
	return domain.Observation{}, false
}

func reportFor(paper domain.Paper, preference string) string {
	sections := []string{
		fmt.Sprintf("# %s", paper.Title),
		fmt.Sprintf("## 一句话理解\n\n这篇论文属于「%s」模块。可以把它理解为：%s", paper.Module, paper.Contribution),
		fmt.Sprintf("## 问题背景\n\n%s 对 Agent 来说，这类问题不会只影响一次回答，还会沿着后续工具调用、记忆写入和计划执行继续传播，所以需要在系统层解决。", paper.Problem),
		fmt.Sprintf("## 核心方法\n\n%s 阅读时要注意方法的输入、状态变化和输出，而不只记住方法名称。", paper.Method),
		fmt.Sprintf("## 为什么重要\n\n%s 这说明 Agent 能力通常来自模型与外部系统的组合，不应只比较基础模型排行榜。", paper.Contribution),
		fmt.Sprintf("## 工程启发\n\n%s 当前解读采用「%s」风格。建议先做一个最小对照实验，保存完整轨迹，再判断该方法是否真的提升成功率或降低成本。", paper.Engineering, preference),
		fmt.Sprintf("## 局限\n\n%s 论文结论应放回其数据集、模型和预算中理解，不能直接外推到所有 Agent 产品。", paper.Limitation),
		fmt.Sprintf("## 原文\n\n%s", paper.URL),
	}
	return strings.Join(sections, "\n\n")
}
