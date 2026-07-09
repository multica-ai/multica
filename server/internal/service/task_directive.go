package service

import (
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
)

const (
	TaskExecutionModeNormal = "normal"
	TaskExecutionModeGoal   = "goal"
)

type PromptDirectiveResult struct {
	ExecutionMode string
	Prompt        string
}

func ParseTaskPromptDirectives(input string) PromptDirectiveResult {
	lineStart := 0
	for lineStart < len(input) {
		lineEnd := strings.IndexByte(input[lineStart:], '\n')
		line := input[lineStart:]
		nextStart := len(input)
		if lineEnd >= 0 {
			nextStart = lineStart + lineEnd + 1
			line = input[lineStart : lineStart+lineEnd]
		}
		lineNoCR := strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(lineNoCR) == "" {
			lineStart = nextStart
			continue
		}
		if strings.TrimSpace(lineNoCR) != "/goal" {
			return PromptDirectiveResult{ExecutionMode: TaskExecutionModeNormal, Prompt: input}
		}
		rest := input[nextStart:]
		for strings.HasPrefix(rest, "\r\n") || strings.HasPrefix(rest, "\n") || strings.HasPrefix(rest, "\r") {
			switch {
			case strings.HasPrefix(rest, "\r\n"):
				rest = rest[2:]
			default:
				rest = rest[1:]
			}
		}
		return PromptDirectiveResult{ExecutionMode: TaskExecutionModeGoal, Prompt: rest}
	}
	return PromptDirectiveResult{ExecutionMode: TaskExecutionModeNormal, Prompt: input}
}

func normalizeTaskExecutionMode(mode string) string {
	if mode == TaskExecutionModeGoal {
		return TaskExecutionModeGoal
	}
	return TaskExecutionModeNormal
}

func taskExecutionModeParam(mode string) pgtype.Text {
	return pgtype.Text{String: normalizeTaskExecutionMode(mode), Valid: true}
}
