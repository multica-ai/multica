package engine

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// ProjectCommand is a deterministic control command. It is parsed without an
// LLM and handled before chat-session creation, so control messages never
// enqueue an Agent task.
type ProjectCommand struct {
	Resource     string
	Action       string
	Arguments    []string
	RawArguments string
}

// ProjectCommandContext carries the authenticated channel context into the
// project command service.
type ProjectCommandContext struct {
	Installation ResolvedInstallation
	UserID       pgtype.UUID
	Message      channel.InboundMessage
	Command      ProjectCommand
}

// ProjectCommandResult is rendered directly by the channel adapter.
type ProjectCommandResult struct {
	ReplyText       string
	IssueID         pgtype.UUID
	IssueNumber     int32
	IssueIdentifier string
	IssueTitle      string
}

// ProjectCommandHandler serves group commands, direct-message shortcuts and
// adapter callbacks through one authorization and business-logic path.
type ProjectCommandHandler interface {
	HandleProjectCommand(ctx context.Context, p ProjectCommandContext) (ProjectCommandResult, error)
}

// ParseProjectCommand recognizes only explicit control prefixes. Unknown
// actions still parse and are returned to the service, which responds with
// deterministic help instead of letting an Agent guess the user's intent.
func ParseProjectCommand(body string) (ProjectCommand, bool) {
	lines := strings.Split(body, "\n")
	first := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			first = i
			break
		}
	}
	if first < 0 {
		return ProjectCommand{}, false
	}

	line := strings.TrimSpace(lines[first])
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ProjectCommand{}, false
	}

	switch fields[0] {
	case "/help":
		if fields[0] != line {
			return ProjectCommand{}, false
		}
		return ProjectCommand{Resource: "help", Action: "show"}, true
	case "/project", "/issue":
	default:
		return ProjectCommand{}, false
	}

	resource := strings.TrimPrefix(fields[0], "/")
	if len(fields) == 1 {
		return ProjectCommand{Resource: resource, Action: "help"}, true
	}

	action := fields[1]
	argStart := 2
	if resource == "issue" && !isIssueCommandAction(action) {
		// /issue MUL-123 is the documented shortcut for /issue bind MUL-123.
		action = "bind"
		argStart = 1
	}

	args := append([]string(nil), fields[argStart:]...)
	raw := strings.Join(args, " ")
	if first+1 < len(lines) {
		tail := strings.TrimRight(strings.Join(lines[first+1:], "\n"), " \t\r\n")
		if tail != "" {
			if raw != "" {
				raw += "\n"
			}
			raw += tail
		}
	}

	return ProjectCommand{
		Resource:     resource,
		Action:       action,
		Arguments:    args,
		RawArguments: raw,
	}, true
}

func isIssueCommandAction(action string) bool {
	switch action {
	case "create", "bind", "unbind", "status", "stop", "show", "help":
		return true
	default:
		return false
	}
}
