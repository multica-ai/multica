package github

import "strings"

// State machine for translating GitHub PR / review events into Multica issue
// status transitions.
//
// The machine is intentionally a pure function: given an Input, it returns a
// Decision. The webhook handler is responsible for I/O (loading the issue,
// querying review state from GitHub) and for applying the Decision.
//
// Status vocabulary (after migration 1010): backlog, todo, in_progress,
// done, blocked, cancelled, planning, ready_for_dev, code_review, fixing,
// testing, coderabbit, resolving, in_review, staged.
//
// CR-resolution flow (Phase 1, post-2026-05-03 redesign):
//
//   testing      → coderabbit   (Marcus dispatched: publish PR; CR reviews)
//   coderabbit   → resolving    (CR submitted review with unresolved threads)
//   coderabbit   → staged       (CR APPROVED on first pass; skips in_review)
//   resolving    → in_review    (Rosa's <!-- resolution-note -->)         [sidecar; Phase 2]
//   in_review    → coderabbit   (Marcus's <!-- pr-republished -->)        [sidecar; Phase 2]
//   coderabbit   → staged       (CR APPROVED — sole path to staged)
//
// Until Phase 2 ships, the sidecar still routes resolving → code_review →
// testing → in_review (Quinn + Murat) and Marcus completes a cycle in
// in_review WITHOUT a state-machine-driven exit — issues will park there
// after Marcus finishes his /resolve loop. Manual completion or a bespoke
// sidecar marker is required to drain the in_review backlog mid-rollout.
//
// Status transition table (this file owns only the GitHub-driven edges):
//
//   Event                                              From            To
//   ---------------------------------------------------------------- ------------
//   pull_request.opened                                pre-in_review   coderabbit
//   review.submitted state=changes_requested (CR bot)  coderabbit      resolving
//   review.submitted (CR bot) + LocalUnresolvedCount>0 coderabbit      resolving
//   review.submitted state=approved (CR bot) + clean   coderabbit      staged
//   review.submitted state=commented + clean           coderabbit      noop  (race guard)
//   pull_request.closed merged=true                    coderabbit      staged → done (chained)
//   pull_request.closed merged=true                    in_review       staged → done (chained)
//   pull_request.closed merged=true                    staged          done
//   pull_request.closed merged=false                   any             blocked
//   pull_request.reopened                              blocked|done    coderabbit
//   pull_request.synchronize                           any             noop (sidecar drives bounce-backs)
//
// Two invariants the table enforces:
//
//   1. APPROVED-only-→-staged: only an explicit CR APPROVED review event
//      can autonomously promote an issue to staged. Thread-resolved events
//      draining the local count to zero are NOT a "CR approved" signal —
//      they are Marcus's own /resolve calls in the in_review loop, and
//      reacting to them would (a) bounce in_review back to resolving on
//      every resolve and (b) violate the "ONLY APPROVED → staged" rule.
//
//   2. Race guard for COMMENTED reviews: GitHub delivers
//      `pull_request_review` ahead of the inline
//      `pull_request_review_comment` events. handleReview's bulk-mirror
//      pulls the review's comments via REST before Decide is called, so
//      LocalUnresolvedThreadCount reflects the full set on the original
//      review event. The per-comment re-eval in handleReviewComment is the
//      fallback when bulk-fetch fails. If both fail, the cr-settle sweeper
//      rescues stuck `coderabbit` issues after MULTICA_CR_SETTLE_SECS.
//
// Anything not listed above is a no-op (Decision.Action == ActionNoop).
//
// Synchronize is intentionally noop in the new flow. The CR-resolution loop
// is driven by review events and sidecar markers, not by raw push events.
// SynchronizeCooldown + IsAgentPusher are kept for documentation and future
// re-use but no longer participate in any transition.

// Action is what the webhook handler should do as a result of an event.
type Action int

const (
	// ActionNoop means: do nothing. Either the event is irrelevant or the
	// issue is already in the target state.
	ActionNoop Action = iota

	// ActionLinkPR records pr_url/pr_number/pr_repo on the issue and sets
	// status to NewStatus. Used on pull_request.opened.
	ActionLinkPR

	// ActionSetStatus changes the issue status to NewStatus. Used for
	// transitions on already-linked PRs (synchronize, review, close, etc.).
	ActionSetStatus

	// ActionRecordPendingApproval records a COMMENTED+clean wrapping review
	// for the settle sweeper without changing issue status.
	ActionRecordPendingApproval

	// ActionSetStatusAndCloseAttempt changes issue status and closes the
	// current CR review attempt atomically.
	ActionSetStatusAndCloseAttempt
)

// Status values referenced by the state machine.
const (
	StatusInProgress = "in_progress"
	StatusCodeReview = "code_review"
	StatusFixing     = "fixing"
	StatusTesting    = "testing"
	StatusCoderabbit = "coderabbit"
	StatusResolving  = "resolving"
	StatusInReview   = "in_review"
	StatusStaged     = "staged"
	StatusBlocked    = "blocked"
	StatusDone       = "done"
)

// SynchronizeCooldown is how long after a pull_request.opened event we ignore
// pull_request.synchronize events on the same PR. GitHub will sometimes fire
// both back-to-back from a single push, and the synchronize would otherwise
// flip in_review → fixing immediately.
const SynchronizeCooldown = 90 // seconds

// AgentPusherLogins is the set of GitHub usernames that represent BMAD
// agents pushing on their own branch. Synchronize events from these logins
// while the issue is in_review are treated as the dev agent's own follow-up
// commit (not a fixing iteration triggered by a reviewer) and ignored.
//
// Keep this list in sync with the GitHub identities of the BMAD dev/architect
// agents (Amelia, Winston, etc.). The match is case-insensitive.
var AgentPusherLogins = map[string]struct{}{
	"bmad-amelia":  {},
	"bmad-winston": {},
	"bmad-quinn":   {},
	"bmad-murat":   {},
}

// IsAgentPusher returns true if login is a BMAD agent identity (case-insensitive).
func IsAgentPusher(login string) bool {
	if login == "" {
		return false
	}
	lower := strings.ToLower(login)
	_, ok := AgentPusherLogins[lower]
	return ok
}

// PRAction maps to GitHub's pull_request.action field.
type PRAction string

const (
	PRActionOpened      PRAction = "opened"
	PRActionReopened    PRAction = "reopened"
	PRActionClosed      PRAction = "closed"
	PRActionSynchronize PRAction = "synchronize"
)

// ReviewState maps to GitHub's pull_request_review.state field.
type ReviewState string

const (
	ReviewChangesRequested ReviewState = "changes_requested"
	ReviewApproved         ReviewState = "approved"
	ReviewCommented        ReviewState = "commented"
)

// EventKind tells the state machine which family of GitHub event we're
// dispatching.
type EventKind int

const (
	EventKindPR EventKind = iota
	EventKindReview
)

// Input is everything the state machine needs to decide. The webhook handler
// fills this in from the GitHub payload + a CR-thread predicate it computed.
type Input struct {
	Kind EventKind

	// Current Multica issue status before the transition.
	IssueStatus string

	// PR-event fields (populated when Kind == EventKindPR).
	PRAction PRAction
	Merged   bool

	// SenderLogin is the GitHub login of the user who triggered the event
	// (payload.sender.login). Used to recognise agent-pusher self-pushes.
	SenderLogin string

	// SecondsSinceOpened is the number of seconds between the PR's opened-at
	// timestamp and the current event. Used to suppress synchronize events
	// that arrive in the immediate aftermath of opened. Zero or negative
	// values disable the cooldown check.
	SecondsSinceOpened int64

	// Review-event fields (populated when Kind == EventKindReview).
	ReviewState ReviewState
	ReviewByCR  bool // review submitted by the configured CR bot

	// CR-thread predicate for the *current* PR state.
	//
	// NoOpenCRChangesRequest is sourced from GitHub's REST reviews API; the
	// handler decides this by walking the review history and finding the
	// latest non-DISMISSED CR review state.
	//
	// NoUnresolvedCRThreads is now sourced from our local issue_review_thread
	// table. We count how many CR-authored threads are in state='unresolved'
	// for this issue. The mirror is kept in sync with GitHub via the
	// pull_request_review_thread.resolved/unresolved webhook events, so the
	// local count converges to GitHub's truth without a GraphQL call.
	NoOpenCRChangesRequest bool // no open review with state=changes_requested from CR bot
	NoUnresolvedCRThreads  bool // zero unresolved review threads from CR bot

	// LocalUnresolvedThreadCount is the count of unresolved CR review threads
	// recorded against this issue in our local issue_review_thread table.
	// Drives the in_review → fixing transition when CR posts inline comments
	// without formally requesting changes (CR's COMMENTED review state).
	//
	// NOTE: This is the same data source as NoUnresolvedCRThreads (which is
	// just `LocalUnresolvedThreadCount == 0`). We keep both fields so the
	// state machine can express "any unresolved" vs "all resolved" cleanly.
	LocalUnresolvedThreadCount int
}

// Decision is the state machine's output.
type Decision struct {
	Action    Action
	NewStatus string

	// ActivityKind is a short label that the webhook handler attaches to
	// the activity row it emits. Empty when Action == ActionNoop.
	ActivityKind string

	AttemptOutcome    string
	AttemptReason     string
	ReviewStateRecord ReviewState
	FindingsCount     int
}

// Decide is the pure transition function. The handler should NEVER mutate
// state outside of what Decide returns.
func Decide(in Input) Decision {
	switch in.Kind {
	case EventKindPR:
		return decidePR(in)
	case EventKindReview:
		return decideReview(in)
	}
	return Decision{Action: ActionNoop}
}

func decidePR(in Input) Decision {
	switch in.PRAction {
	case PRActionOpened:
		// First push from Marcus opens the PR; the issue moves to the
		// `coderabbit` column where CR reviews. Link metadata is recorded
		// regardless. If the issue is already at or past coderabbit (e.g.
		// re-delivery of an opened event), preserve the existing status.
		if isAtOrPast(in.IssueStatus, StatusCoderabbit) {
			return Decision{
				Action:       ActionLinkPR,
				NewStatus:    in.IssueStatus, // preserve
				ActivityKind: "pr_opened",
			}
		}
		return Decision{
			Action:       ActionLinkPR,
			NewStatus:    StatusCoderabbit,
			ActivityKind: "pr_opened",
		}

	case PRActionSynchronize:
		// Synchronize is intentionally noop. Bounce-backs through the CR
		// loop are driven by CR review events (handled in decideReview)
		// and sidecar markers, not by raw push events. Marcus pushes are
		// suppressed by the agent-pusher carve-out historically; in the
		// new model we just don't react to pushes at all.
		return Decision{Action: ActionNoop}

	case PRActionClosed:
		if in.Merged {
			if in.IssueStatus == StatusDone {
				return Decision{Action: ActionNoop}
			}
			// Preserve the staged audit step. User-expected flow:
			//   CR signals "ready to merge" → staged → human merges → done.
			// If a merge arrives while the issue is still at coderabbit
			// or in_review (e.g. CR not installed on the repo, or merge
			// races the predicate), transition first to staged with a
			// distinct activity kind; the chained applyDecision logic in
			// the webhook handler runs the staged → done flip in the same
			// turn (see webhook_handler.go).
			if in.IssueStatus == StatusStaged {
				return Decision{
					Action:       ActionSetStatus,
					NewStatus:    StatusDone,
					ActivityKind: "pr_merged",
				}
			}
			if in.IssueStatus == StatusCoderabbit {
				return Decision{
					Action:       ActionSetStatus,
					NewStatus:    StatusStaged,
					ActivityKind: "pr_merged_from_coderabbit",
				}
			}
			if in.IssueStatus == StatusInReview {
				return Decision{
					Action:       ActionSetStatus,
					NewStatus:    StatusStaged,
					ActivityKind: "pr_merged_from_in_review",
				}
			}
			return Decision{
				Action:       ActionSetStatus,
				NewStatus:    StatusDone,
				ActivityKind: "pr_merged",
			}
		}
		if in.IssueStatus == StatusBlocked {
			return Decision{Action: ActionNoop}
		}
		return Decision{
			Action:       ActionSetStatus,
			NewStatus:    StatusBlocked,
			ActivityKind: "pr_closed_unmerged",
		}

	case PRActionReopened:
		// Re-opening a closed/done PR re-enters the CR cycle from the
		// top — i.e. coderabbit, where Marcus's republish + CR's review
		// will drive the next state.
		if in.IssueStatus == StatusBlocked || in.IssueStatus == StatusDone {
			return Decision{
				Action:       ActionSetStatus,
				NewStatus:    StatusCoderabbit,
				ActivityKind: "pr_reopened",
			}
		}
		return Decision{Action: ActionNoop}
	}
	return Decision{Action: ActionNoop}
}

func decideReview(in Input) Decision {
	if !in.ReviewByCR {
		return Decision{Action: ActionNoop}
	}
	if in.IssueStatus != StatusCoderabbit {
		return Decision{Action: ActionNoop}
	}
	if in.ReviewState == ReviewChangesRequested {
		return Decision{
			Action:            ActionSetStatusAndCloseAttempt,
			NewStatus:         StatusResolving,
			ActivityKind:      "review_changes_requested",
			AttemptOutcome:    "completed_with_findings",
			AttemptReason:     "changes_requested",
			ReviewStateRecord: ReviewChangesRequested,
			FindingsCount:     in.LocalUnresolvedThreadCount,
		}
	}
	if in.ReviewState == ReviewCommented && in.LocalUnresolvedThreadCount > 0 {
		return Decision{
			Action:            ActionSetStatusAndCloseAttempt,
			NewStatus:         StatusResolving,
			ActivityKind:      "review_comments_unresolved",
			AttemptOutcome:    "completed_with_findings",
			AttemptReason:     "commented_with_unresolved",
			ReviewStateRecord: ReviewCommented,
			FindingsCount:     in.LocalUnresolvedThreadCount,
		}
	}
	if in.ReviewState == ReviewApproved && in.NoOpenCRChangesRequest && in.NoUnresolvedCRThreads {
		return Decision{
			Action:            ActionSetStatusAndCloseAttempt,
			NewStatus:         StatusStaged,
			ActivityKind:      "review_passed",
			AttemptOutcome:    "completed_clean",
			AttemptReason:     "approved_clean",
			ReviewStateRecord: ReviewApproved,
			FindingsCount:     0,
		}
	}
	if in.ReviewState == ReviewCommented && in.NoOpenCRChangesRequest && in.NoUnresolvedCRThreads {
		return Decision{
			Action:            ActionRecordPendingApproval,
			ActivityKind:      "review_commented_clean_pending",
			ReviewStateRecord: ReviewCommented,
			FindingsCount:     0,
		}
	}
	return Decision{Action: ActionNoop}
}

// isAtOrPast returns true when current is at or past target in the
// PR-driven status progression: coderabbit → resolving → in_review →
// staged → done. Anything else (todo/in_progress/code_review/fixing/
// testing/blocked) counts as "before" — those columns are inner-loop
// work that doesn't touch the CR cycle's monotone ordering.
//
// This is used by PRActionOpened to avoid demoting a PR that's already
// progressed past the initial coderabbit column (e.g. on webhook
// redelivery after the issue has already moved through resolving).
func isAtOrPast(current, target string) bool {
	rank := map[string]int{
		StatusCoderabbit: 1,
		StatusResolving:  2,
		StatusInReview:   3,
		StatusStaged:     4,
		StatusDone:       5,
	}
	c, cok := rank[current]
	t, tok := rank[target]
	if !cok || !tok {
		return false
	}
	return c >= t
}
