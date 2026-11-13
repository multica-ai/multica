package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
)

// steerTarget is one running turn a comment is added to.
type steerTarget struct {
	TaskID    string
	AgentID   string
	AgentName string
}

// resolveSteerTargets turns --steer run IDs and --steer-active into the runs
// the comment is sent to. An explicit target may be any run of this issue: one
// that ended since the caller looked is still sent, so the server gives its
// agent the normal trigger instead of the comment being lost. The caller's
// own runs are never targets: the server refuses a self-steer. --steer-active
// picks the runs the server marks can_supplement, which it does only for a
// running turn that takes additional messages and whose agent this caller may
// invoke.
func resolveSteerTargets(ctx context.Context, client *cli.APIClient, issueID string, inputs []string, active bool) ([]steerTarget, error) {
	if len(inputs) == 0 && !active {
		return nil, nil
	}
	var runs []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+url.PathEscape(issueID)+"/task-runs", &runs); err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	selfTask, selfAgent := os.Getenv("MULTICA_TASK_ID"), os.Getenv("MULTICA_AGENT_ID")
	isSelf := func(run map[string]any) bool {
		return (selfTask != "" && strVal(run, "id") == selfTask) || (selfAgent != "" && strVal(run, "agent_id") == selfAgent)
	}
	byID := make(map[string]map[string]any, len(runs))
	candidates := make([]idCandidate, 0, len(runs))
	for _, run := range runs {
		id := strVal(run, "id")
		byID[id] = run
		candidates = append(candidates, idCandidate{ID: id, Display: id})
	}
	// Short prefixes resolve against the runs already fetched: the same
	// candidates `issue cancel-task` would load for this issue.
	fromIssue := func(context.Context, *cli.APIClient) ([]idCandidate, error) { return candidates, nil }

	var targets []steerTarget
	seen := make(map[string]bool)
	add := func(run map[string]any) {
		id := strVal(run, "id")
		if !seen[id] {
			seen[id] = true
			targets = append(targets, steerTarget{TaskID: id, AgentID: strVal(run, "agent_id")})
		}
	}
	for _, input := range inputs {
		ref, err := resolveIDByPrefix(ctx, client, "task run", input, fromIssue)
		if err != nil {
			return nil, fmt.Errorf("--steer %s: %w", input, err)
		}
		run, ok := byID[ref.ID]
		if !ok {
			return nil, fmt.Errorf("--steer %s: not a run of this issue", input)
		}
		if isSelf(run) {
			return nil, fmt.Errorf("--steer %s: an agent cannot steer its own run", input)
		}
		add(run)
	}
	if active {
		for _, run := range runs {
			if run["can_supplement"] == true && !isSelf(run) {
				add(run)
			}
		}
		if len(targets) == 0 {
			fmt.Fprintln(os.Stderr, "No running run on this issue that you may steer; posting a normal comment.")
		}
	}
	for i := range targets {
		targets[i].AgentName = steerAgentName(ctx, client, targets[i].AgentID)
	}
	return targets, nil
}

func steerAgentName(ctx context.Context, client *cli.APIClient, agentID string) string {
	var agent map[string]any
	if err := client.GetJSON(ctx, "/api/agents/"+url.PathEscape(agentID), &agent); err == nil {
		if name := strings.TrimSpace(strVal(agent, "name")); name != "" {
			return name
		}
	}
	return "agent"
}

// withSteerMentions makes every steered agent a recipient of the comment.
// The server only steers agents the comment triggers, and an agent's comment
// triggers only the agents it mentions; the mention is also what falls back
// to a normal trigger, behind the same invoke check, when the turn cannot
// take the message.
func withSteerMentions(content string, targets []steerTarget) string {
	var prefix []string
	mentioned := make(map[string]bool)
	for _, target := range targets {
		if mentioned[target.AgentID] || strings.Contains(content, "mention://agent/"+target.AgentID+")") {
			continue
		}
		mentioned[target.AgentID] = true
		name := strings.NewReplacer("[", "", "]", "").Replace(target.AgentName)
		prefix = append(prefix, fmt.Sprintf("[@%s](mention://agent/%s)", name, target.AgentID))
	}
	if len(prefix) == 0 {
		return content
	}
	return strings.Join(prefix, " ") + " " + content
}

// reportSteerOutcomes says, per target, whether the running turn took the
// message or the agent got the normal trigger instead. With --steer-active the
// server may also steer a recipient that was not listed (its run started after
// the listing); those are reported by agent id.
func reportSteerOutcomes(w io.Writer, targets []steerTarget, result map[string]any) {
	fallback := "if the turn ends before reading it, this comment is handled in a follow-up run"
	if os.Getenv("MULTICA_AGENT_ID") == "" {
		fallback = "the comment shows whether it was read, and offers a retry if it was not"
	}
	outcomes, _ := result["trigger_outcomes"].([]any)
	byAgent := make(map[string]map[string]any, len(outcomes))
	for _, raw := range outcomes {
		if outcome, ok := raw.(map[string]any); ok && strVal(outcome, "target_type") == "agent" {
			byAgent[strVal(outcome, "target_id")] = outcome
		}
	}
	reported := make(map[string]bool, len(targets))
	for _, target := range targets {
		reported[target.AgentID] = true
		outcome := byAgent[target.AgentID]
		switch status := strVal(outcome, "status"); status {
		case "steered":
			fmt.Fprintf(w, "Steered run %s (%s): added to the running turn, which reads it at its next step; %s.\n", target.TaskID, target.AgentName, fallback)
		case "blocked":
			fmt.Fprintf(w, "Not steered run %s (%s): blocked (%s); the agent was not triggered.\n", target.TaskID, target.AgentName, strVal(outcome, "reason_code"))
		case "":
			fmt.Fprintf(w, "Not steered run %s (%s): the comment did not trigger this agent.\n", target.TaskID, target.AgentName)
		default:
			fmt.Fprintf(w, "Not steered run %s (%s): %s; the agent handles this comment in a follow-up run.\n", target.TaskID, target.AgentName, status)
		}
	}
	for agentID, outcome := range byAgent {
		if !reported[agentID] && strVal(outcome, "status") == "steered" {
			fmt.Fprintf(w, "Steered agent %s: added to its running turn, which reads it at its next step; %s.\n", agentID, fallback)
		}
	}
}
