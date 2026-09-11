package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestDispatchFailReasonCode is the regression for Elon must-fix 2, case 1: a
// dispatch that fails fail-closed on attribution must be classified
// attribution_blocked, not internal_error — decided by a TYPED errors.Is check,
// not by substring-matching an English message.
func TestDispatchFailReasonCode(t *testing.T) {
	if got := dispatchFailReasonCode(ErrAttributionFailClosed); got != dispatch.ReasonAttributionBlocked {
		t.Errorf("bare fail-closed: got %q, want attribution_blocked", got)
	}
	// The real dispatch path wraps the sentinel (create_issue enqueue → %w);
	// errors.Is must still see through the wrap.
	wrapped := fmt.Errorf("dispatch create_issue: enqueue task for issue: %w", ErrAttributionFailClosed)
	if got := dispatchFailReasonCode(wrapped); got != dispatch.ReasonAttributionBlocked {
		t.Errorf("wrapped fail-closed: got %q, want attribution_blocked", got)
	}
	if got := dispatchFailReasonCode(errors.New("some other failure")); got != dispatch.ReasonInternalError {
		t.Errorf("generic error: got %q, want internal_error", got)
	}
}

// TestAgentReadinessVerdict is the regression for Elon must-fix 2, case 2: a
// runtime-availability failure must be classified from the agent's and
// runtime's own state, not from the reason text. The three runtime failures are
// deliberately distinct — an agent with NO runtime needs to be bound to one
// (agent_runtime_required); a bound-but-offline runtime needs the machine back
// (runtime_offline); a runtime whose CLI cannot be executed needs a reinstall
// on a machine that is already connected (runtime_unusable, MUL-6164).
// Collapsing any pair sends the user to fix the wrong thing.
func TestAgentReadinessVerdict(t *testing.T) {
	validRuntime := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	archivedAt := pgtype.Timestamptz{Valid: true}

	// The agent-only half needs no runtime row, so AgentReadiness answers before
	// it queries one.
	if got, _ := AgentReadiness(t.Context(), RuntimeLookup{}, db.Agent{}); got.Reason != dispatch.ReasonAgentRuntimeRequired || !got.Blocked() {
		t.Errorf("no runtime bound: got %+v, want blocked/agent_runtime_required", got)
	}
	if got, _ := AgentReadiness(t.Context(), RuntimeLookup{}, db.Agent{ArchivedAt: archivedAt, RuntimeID: validRuntime}); got.Reason != dispatch.ReasonTargetUnavailable || !got.Blocked() {
		t.Errorf("archived agent: got %+v, want blocked/target_unavailable", got)
	}

	// The runtime half.
	if got := runtimeVerdict(db.AgentRuntime{Status: "online"}); !got.Ready() {
		t.Errorf("online runtime: got %+v, want ready", got)
	}
	// Offline with no explanation is the sleeping-laptop case: work waits for it,
	// so this must NOT be blocked.
	offline := runtimeVerdict(db.AgentRuntime{Status: "offline"})
	if offline.Blocked() || offline.Reason != dispatch.ReasonRuntimeOffline {
		t.Errorf("plain offline runtime: got %+v, want waitable/runtime_offline", offline)
	}
	// Offline because the CLI cannot be executed: waiting is futile, and the
	// repair the daemon reported has to survive to the caller — it is the only
	// thing that tells the user what to actually do.
	unusable := runtimeVerdict(db.AgentRuntime{
		Status:   "offline",
		Metadata: []byte(`{"offline_reason":{"code":"not_executable","detail":"agent CLI is not executable","repair":{"package":"@anthropic-ai/claude-code","command":"cd '/pkg' && node install.cjs"}}}`),
	})
	if !unusable.Blocked() || unusable.Reason != dispatch.ReasonRuntimeUnusable {
		t.Fatalf("unusable runtime: got %+v, want blocked/runtime_unusable", unusable)
	}
	if unusable.Repair == nil || unusable.Repair.Command != "cd '/pkg' && node install.cjs" {
		t.Errorf("unusable runtime lost the repair command: got %+v", unusable.Repair)
	}
	// Offline because the DSH runtime profile is missing is blocked for the same
	// reason — the machine is reachable, its CLI cannot serve work, and no
	// amount of waiting installs a bundle nobody configured — but under its OWN
	// code. Collapsing it into runtime_unusable is what made every client tell
	// the user to reinstall a CLI that runs perfectly.
	profile := runtimeVerdict(db.AgentRuntime{
		Status:   "offline",
		Metadata: []byte(`{"offline_reason":{"code":"dsh_profile","detail":"the Multica runtime profile is not installed","repair":{"package":"DeepSeek Harness runtime profile"}}}`),
	})
	if !profile.Blocked() || profile.Reason != dispatch.ReasonRuntimeProfileMissing {
		t.Fatalf("missing DSH profile: got %+v, want blocked/runtime_profile_missing", profile)
	}
	if !RuntimeBlockedNeedsNotice(profile.Reason) {
		t.Error("missing DSH profile would leave no durable trace on the issue")
	}
	// The notice must describe the profile, not a broken CLI, and must not hand
	// the user a command with a placeholder in it to paste.
	notice := RuntimeUnusableNotice("Kit", profile)
	if !strings.Contains(notice, "runtime profile") {
		t.Errorf("notice does not name the missing profile: %q", notice)
	}
	for _, wrong := range []string{"cannot be executed", "postinstall", "<bundle>", "```"} {
		if strings.Contains(notice, wrong) {
			t.Errorf("notice contains %q, which belongs to the unrunnable-CLI repair: %q", wrong, notice)
		}
	}
	// The unrunnable-CLI notice keeps its own text and its fenced command.
	if unusableNotice := RuntimeUnusableNotice("Kit", unusable); !strings.Contains(unusableNotice, "node install.cjs") {
		t.Errorf("unusable-CLI notice lost its repair command: %q", unusableNotice)
	}
	// The installing claim is bounded, not trusted. A withdraw cannot be
	// guaranteed — the verdict is built before the runtime ids it needs are
	// known, the corrective Deregister is best-effort and runs on a cancelled
	// context during shutdown, and a daemon restarted mid-install does not track
	// the row at all — so a claim older than the install it describes must stop
	// buying a queue, or it recreates the failure it was added to prevent.
	installingRow := func(updatedAt time.Time) db.AgentRuntime {
		return db.AgentRuntime{
			Status:    "offline",
			Metadata:  []byte(`{"offline_reason":{"code":"dsh_profile","installing":true,"detail":"installing the configured bundle now"}}`),
			UpdatedAt: pgtype.Timestamptz{Time: updatedAt, Valid: true},
		}
	}
	// The one exception is an install the daemon is running right now: that wait
	// DOES end by itself, so the work queues instead of being refused.
	fresh := runtimeVerdict(installingRow(time.Now()))
	if fresh.Blocked() {
		t.Errorf("install in flight: got %+v, want the waitable verdict", fresh)
	}
	if fresh.Reason != dispatch.ReasonRuntimeOffline {
		t.Errorf("install in flight: reason = %q, want %q", fresh.Reason, dispatch.ReasonRuntimeOffline)
	}
	stale := runtimeVerdict(installingRow(time.Now().Add(-runtimeInstallClaimWindow - time.Minute)))
	if !stale.Blocked() || stale.Reason != dispatch.ReasonRuntimeProfileMissing {
		t.Fatalf("stale install claim: got %+v, want blocked/runtime_profile_missing", stale)
	}
	// No timestamp to bound it with: refuse rather than queue behind something
	// that cannot be shown to be running.
	unbounded := runtimeVerdict(db.AgentRuntime{
		Status:   "offline",
		Metadata: []byte(`{"offline_reason":{"code":"dsh_profile","installing":true}}`),
	})
	if !unbounded.Blocked() {
		t.Errorf("install claim with no updated_at: got %+v, want blocked", unbounded)
	}

	// An unrecognised or malformed reason must not invent a verdict: unknown
	// causes stay in the waitable bucket they are in today.
	for name, metadata := range map[string]string{
		"unknown code": `{"offline_reason":{"code":"who_knows"}}`,
		"malformed":    `{"offline_reason":`,
		"empty":        ``,
	} {
		got := runtimeVerdict(db.AgentRuntime{Status: "offline", Metadata: []byte(metadata)})
		if got.Blocked() {
			t.Errorf("%s: got %+v, want the plain offline verdict", name, got)
		}
	}
}

// The notice is the only thing a user sees when an assignment or an
// agent-authored mention is refused, so it has to carry the repair command when
// there is one and stay actionable when there is not.
func TestRuntimeUnusableNotice(t *testing.T) {
	withRepair := RuntimeUnusableNotice("Mika", AgentVerdict{
		Repair: &RuntimeRepair{Package: "@anthropic-ai/claude-code", Command: "cd '/pkg' && node install.cjs"},
	})
	if !strings.Contains(withRepair, "Mika") || !strings.Contains(withRepair, "cd '/pkg' && node install.cjs") {
		t.Errorf("notice must name the agent and the repair command:\n%s", withRepair)
	}
	// The fence is labelled with the shell the command was written for: a
	// Windows repair shown as bash is how a user ends up pasting `cd 'C:\...'`
	// into PowerShell.
	if !strings.Contains(withRepair, "```bash") {
		t.Errorf("a bash repair must be fenced as bash:\n%s", withRepair)
	}
	windows := RuntimeUnusableNotice("Mika", AgentVerdict{
		Repair: &RuntimeRepair{Command: "Set-Location 'C:\\pkg'\nnode install.cjs", Shell: "powershell"},
	})
	if !strings.Contains(windows, "```powershell") {
		t.Errorf("a PowerShell repair must be fenced as powershell:\n%s", windows)
	}

	withoutRepair := RuntimeUnusableNotice("Mika", AgentVerdict{})
	if strings.Contains(withoutRepair, "```") {
		t.Errorf("no repair command is known, so none may be shown:\n%s", withoutRepair)
	}
	if !strings.Contains(withoutRepair, "Reinstall") {
		t.Errorf("notice must still say what to do:\n%s", withoutRepair)
	}
}
