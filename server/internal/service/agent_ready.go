package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/multica-ai/multica/server/internal/dispatch"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AgentAvailability is what a readiness check concluded, in the vocabulary the
// callers actually branch on.
//
// The distinction that matters is not "ready or not" but whether WAITING is a
// plan. A sleeping laptop comes back on its own, so queued work is right to
// wait for it; an agent bound to nothing, or bound to a machine whose CLI
// cannot run, will never pick that work up until a human acts. dispatch's
// reason codes already draw that line — this type is the server-side half so
// every admission path draws it the same way.
type AgentAvailability int

const (
	// AgentAvailable: the agent can take work now.
	AgentAvailable AgentAvailability = iota
	// AgentWaitable: not runnable right now, but nothing is broken — the
	// machine is offline and work queued for it runs when it returns.
	AgentWaitable
	// AgentBlocked: nothing will claim this agent's work until someone
	// intervenes. Callers must refuse the trigger and say why.
	AgentBlocked
)

// AgentVerdict is a readiness decision plus everything a caller needs to act on
// it: the wire reason code, and — when the daemon reported one — the command
// that repairs the runtime.
type AgentVerdict struct {
	Availability AgentAvailability
	// Reason is the dispatch code for a non-available verdict, empty otherwise.
	Reason dispatch.ReasonCode
	// Repair is the fix the daemon reported for an unusable runtime: the npm
	// package that owns the broken entry point and the command that reinstalls
	// it. Absent for every other verdict, and for a runtime whose daemon is too
	// old to report one.
	Repair *RuntimeRepair
	// Detail is the daemon's own description, for logs and for the record left
	// on a blocked non-interactive trigger. Never parsed.
	Detail string
}

// Ready reports whether the agent can take work right now.
func (v AgentVerdict) Ready() bool { return v.Availability == AgentAvailable }

// Blocked reports whether waiting is futile and the caller must refuse the
// trigger rather than queue it.
func (v AgentVerdict) Blocked() bool { return v.Availability == AgentBlocked }

// RuntimeRepair mirrors the daemon's agent.ExecFormatRepair over the wire.
type RuntimeRepair struct {
	Package string `json:"package,omitempty"`
	Command string `json:"command,omitempty"`
	// Shell is the interpreter Command is written for, as reported by the
	// machine that must run it ("bash", "powershell"). Rendered as the code
	// block's language so a Windows user is not handed POSIX syntax in a bash
	// fence. Empty from a daemon too old to report it.
	Shell string `json:"shell,omitempty"`
}

// runtimeOfflineReason is the daemon's structured explanation, stored on the
// runtime row's metadata by the deregister handler.
type runtimeOfflineReason struct {
	Code   string         `json:"code"`
	Detail string         `json:"detail"`
	Repair *RuntimeRepair `json:"repair"`
}

// runtimeOfflineCodeNotExecutable is the daemon's code for "the OS refuses to
// execute this agent CLI" (daemon.RuntimeOfflineCodeNotExecutable). Compared as
// a string because the server must not import the daemon package.
const runtimeOfflineCodeNotExecutable = "not_executable"

// AgentReadiness reports whether an agent can accept new work right now, and
// what the caller should do when it cannot.
//
// err is non-nil only on DB lookup failure for the runtime row. Callers that
// treat a transient DB error as "do not skip" (the autopilot admission gate)
// should swallow it; callers that need a hard yes/no (the squad-leader
// pre-enqueue check in the handler) should fail closed.
//
// This is the single source of truth shared by:
//   - service.shouldSkipDispatch (autopilot admission gate)
//   - service.dispatchRunOnly    (squad-leader runtime check, MUL-2429)
//   - handler.isSquadLeaderReady (issue-assign / comment-trigger path)
//   - the direct-agent trigger paths, which consult it for the BLOCKED verdict
//     only: an offline machine still queues, because that wait ends by itself.
//
// Keeping these aligned matters because the paths can otherwise drift — e.g.
// one starts allowing "starting" runtimes while another doesn't, and the bug
// only surfaces when a user assigns the same agent through two different entry
// points. Touch this function, all of them move together.
func AgentReadiness(ctx context.Context, q *db.Queries, agent db.Agent) (AgentVerdict, error) {
	if agent.ArchivedAt.Valid {
		return AgentVerdict{
			Availability: AgentBlocked,
			Reason:       dispatch.ReasonTargetUnavailable,
			Detail:       "agent is archived",
		}, nil
	}
	if !agent.RuntimeID.Valid {
		return AgentVerdict{
			Availability: AgentBlocked,
			Reason:       dispatch.ReasonAgentRuntimeRequired,
			Detail:       "agent has no runtime bound",
		}, nil
	}
	rt, err := q.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		return AgentVerdict{}, err
	}
	return runtimeVerdict(rt), nil
}

// runtimeVerdict is the half of the decision that depends only on the runtime
// row, split out so every branch is testable without a database.
func runtimeVerdict(rt db.AgentRuntime) AgentVerdict {
	if rt.Status == "online" {
		return AgentVerdict{Availability: AgentAvailable}
	}
	// Offline with a reason the daemon says a human must repair: refuse rather
	// than queue, and carry the repair so the caller can show it.
	if reason, ok := parseRuntimeOfflineReason(rt.Metadata); ok && reason.Code == runtimeOfflineCodeNotExecutable {
		return AgentVerdict{
			Availability: AgentBlocked,
			Reason:       dispatch.ReasonRuntimeUnusable,
			Repair:       reason.Repair,
			Detail:       reason.Detail,
		}
	}
	return AgentVerdict{
		Availability: AgentWaitable,
		Reason:       dispatch.ReasonRuntimeOffline,
		Detail:       "agent runtime is " + rt.Status,
	}
}

// parseRuntimeOfflineReason reads the daemon's explanation off a runtime row.
// A row with no reason, or metadata this server cannot parse, simply has no
// explanation — never a different verdict.
func parseRuntimeOfflineReason(metadata []byte) (runtimeOfflineReason, bool) {
	if len(metadata) == 0 {
		return runtimeOfflineReason{}, false
	}
	var envelope struct {
		OfflineReason *runtimeOfflineReason `json:"offline_reason"`
	}
	if err := json.Unmarshal(metadata, &envelope); err != nil || envelope.OfflineReason == nil {
		return runtimeOfflineReason{}, false
	}
	return *envelope.OfflineReason, true
}

// RuntimeUnusableNotice is the durable explanation left on an issue when a
// trigger is refused because the target's agent CLI cannot run on its machine.
//
// It lives here, next to the verdict, because two layers write it: the handler
// for a refused @mention and the service for a refused assignment. One text,
// one place to fix it. It names the repair command when the daemon reported
// one and stays useful when it did not — a natively installed CLI has no
// postinstall to re-run, and inventing a command would send the user somewhere
// that does not exist.
func RuntimeUnusableNotice(agentName string, verdict AgentVerdict) string {
	name := agentName
	if name == "" {
		name = "The assigned agent"
	}
	if verdict.Repair != nil && verdict.Repair.Command != "" {
		return fmt.Sprintf(
			"%s could not start: its CLI is installed but cannot be executed on that machine, so this trigger was not queued.\n\n"+
				"Usually the package's postinstall was blocked (npm 12 allowScripts, pnpm 10 approve-builds, `--ignore-scripts`, `--omit=optional`) and the bin entry is still a placeholder. On that machine, run:\n\n"+
				"```%s\n%s\n```\n\n"+
				"The runtime comes back on its own within a couple of minutes; trigger the agent again after that.",
			name, repairFenceLanguage(verdict.Repair.Shell), verdict.Repair.Command,
		)
	}
	return fmt.Sprintf(
		"%s could not start: its CLI is installed but cannot be executed on that machine, so this trigger was not queued. "+
			"Reinstall the agent CLI on that machine with install scripts enabled; the runtime comes back on its own within a couple of minutes.",
		name,
	)
}

// repairFenceLanguage labels the code block with the shell the command was
// written for. A daemon too old to report one predates the Windows rendering
// entirely, so its command is POSIX by construction.
func repairFenceLanguage(shell string) string {
	if shell == "powershell" {
		return "powershell"
	}
	return "bash"
}
