package github

import "strings"

// State machine for translating GitHub PR / review events into Multica issue
// status transitions.
//
// The machine is intentionally a pure function: given an Input, it returns a
// Decision. The webhook handler is responsible for I/O (loading the issue,
// querying review state from GitHub) and for applying the Decision.
//
// Status vocabulary (after migration 1002): backlog, todo, in_progress,
// in_review, done, blocked, cancelled, planning, ready_for_dev, code_review,
// fixing, testing, staged.
//
// Status transition table (BMAD spec):
//
//   Event                                              From            To
//   ---------------------------------------------------------------- ------------
//   pull_request.opened                                pre-in_review   in_review
//   pull_request.synchronize                           in_review       fixing
//   review.submitted state=changes_requested (CR bot)  in_review       fixing
//   review.submitted (any other CR signal) +
//     no open CHANGES_REQUESTED + no unresolved
//     CR threads                                       in_review       staged
//   review_thread (any) + same predicate above         in_review       staged
//   pull_request.closed merged=true                    any             done
//   pull_request.closed merged=false                   any             blocked
//   pull_request.reopened                              blocked|done    in_review
//
// Anything not listed above is a no-op (Decision.Action == ActionNoop).
//
// Agent-pusher carve-outs (Bug 1, 2026-04-27):
//
//   1. Synchronize events whose sender.login is in AgentPusherLogins are
//      ignored — they reflect the dev agent's own follow-up commit on the
//      branch they just opened the PR from, not a fixing iteration.
//   2. Synchronize events arriving within SynchronizeCooldown of the
//      pull_request.opened event for the same PR are also ignored — GitHub
//      sometimes fires opened+synchronize back-to-back from one push.

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
)

// Status values referenced by the state machine.
const (
	StatusInProgress = "in_progress"
	StatusInReview   = "in_review"
	StatusFixing     = "fixing"
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
	EventKindReviewThread
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
}

// Decide is the pure transition function. The handler should NEVER mutate
// state outside of what Decide returns.
func Decide(in Input) Decision {
	switch in.Kind {
	case EventKindPR:
		return decidePR(in)
	case EventKindReview:
		return decideReview(in)
	case EventKindReviewThread:
		return decideReviewThread(in)
	}
	return Decision{Action: ActionNoop}
}

func decidePR(in Input) Decision {
	switch in.PRAction {
	case PRActionOpened:
		// Always link, even if the issue is already past in_review — the
		// link metadata is useful regardless. But keep the status if it's
		// already at or past in_review, to avoid demoting a staged issue.
		if isAtOrPast(in.IssueStatus, StatusInReview) {
			return Decision{
				Action:       ActionLinkPR,
				NewStatus:    in.IssueStatus, // preserve
				ActivityKind: "pr_opened",
			}
		}
		return Decision{
			Action:       ActionLinkPR,
			NewStatus:    StatusInReview,
			ActivityKind: "pr_opened",
		}

	case PRActionSynchronize:
		// Agent pushed a new commit while on in_review. Per the BMAD spec
		// this means a fixing iteration is in flight — move to `fixing`.
		// CodeRabbit will re-review automatically; on a clean pass the
		// review handler flips back through in_review -> staged.
		//
		// Bug 1 carve-outs: ignore the synchronize when (a) the sender is
		// a BMAD agent pushing on its own branch, or (b) the synchronize
		// landed within SynchronizeCooldown of the pull_request.opened
		// event for the same PR (GitHub double-fires from a single push).
		if in.IssueStatus == StatusInReview {
			if IsAgentPusher(in.SenderLogin) {
				return Decision{Action: ActionNoop}
			}
			if in.SecondsSinceOpened > 0 && in.SecondsSinceOpened < SynchronizeCooldown {
				return Decision{Action: ActionNoop}
			}
			return Decision{
				Action:       ActionSetStatus,
				NewStatus:    StatusFixing,
				ActivityKind: "pr_updated",
			}
		}
		return Decision{Action: ActionNoop}

	case PRActionClosed:
		if in.Merged {
			if in.IssueStatus == StatusDone {
				return Decision{Action: ActionNoop}
			}
			// Bug D fix (2026-04-28): preserve the staged audit step.
			//
			// User-expected flow:
			//   CR signals "ready to merge" → staged (handled in
			//   decideReview when ReviewByCR=true) → human merges PR →
			//   done.
			//
			// Two real-world scenarios where this used to skip staged:
			//   1. CR is NOT installed on the repo (e.g. zeyad-farrag/
			//      TimeTrack). The decideReview path that flips
			//      in_review → staged never fires. Human merges directly
			//      from in_review. We must still pass through staged so
			//      the audit trail and any staged-only automation
			//      (deploys, notifications) get a chance to run.
			//   2. CR is installed but the merge happens before the
			//      staged predicate fires (race or bypass).
			//
			// Implementation: if the merge arrives while the issue is
			// still at `in_review`, transition first to `staged` and
			// emit a distinct activity kind. The webhook handler will
			// re-receive (or in some cases is responsible for emitting
			// a follow-up) — and a future PR-closed event with current
			// status `staged` flips to `done`. In the simple no-CR case,
			// the staged → done flip happens via the same merge event
			// being re-evaluated by an idempotent staged-to-done helper
			// that the handler runs after applying any in_review →
			// staged decision (see webhook_handler.go ApplyDecision).
			if in.IssueStatus == StatusStaged {
				return Decision{
					Action:       ActionSetStatus,
					NewStatus:    StatusDone,
					ActivityKind: "pr_merged",
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
		if in.IssueStatus == StatusBlocked || in.IssueStatus == StatusDone {
			return Decision{
				Action:       ActionSetStatus,
				NewStatus:    StatusInReview,
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

	if in.ReviewState == ReviewChangesRequested {
		// CodeRabbit formally requested changes — bounce to `fixing` so
		// Amelia addresses the feedback. The PR-loop counter (sidecar)
		// handles the cap-2 escalation to `blocked` after repeated cycles.
		if in.IssueStatus == StatusFixing {
			return Decision{Action: ActionNoop}
		}
		return Decision{
			Action:       ActionSetStatus,
			NewStatus:    StatusFixing,
			ActivityKind: "review_changes_requested",
		}
	}

	// Non-CHANGES review (approved / commented).
	//
	// New behaviour (step 2): if CR left a COMMENTED review with at least one
	// unresolved inline thread, treat it as soft-changes-requested and bounce
	// to `fixing`. CR's COMMENTED state is its way of leaving nits/issues
	// without formally blocking the PR — but the BMAD pipeline contract
	// requires the dev agent to walk every comment, so we treat it the same
	// as CHANGES_REQUESTED for orchestration purposes.
	if in.IssueStatus == StatusInReview && in.LocalUnresolvedThreadCount > 0 {
		return Decision{
			Action:       ActionSetStatus,
			NewStatus:    StatusFixing,
			ActivityKind: "review_comments_unresolved",
		}
	}

	// Otherwise re-evaluate the staged predicate: only flip if we're
	// currently in_review and CR has nothing outstanding.
	if in.IssueStatus == StatusInReview &&
		in.NoOpenCRChangesRequest &&
		in.NoUnresolvedCRThreads {
		return Decision{
			Action:       ActionSetStatus,
			NewStatus:    StatusStaged,
			ActivityKind: "review_passed",
		}
	}
	return Decision{Action: ActionNoop}
}

func decideReviewThread(in Input) Decision {
	// Thread-level events (resolved / unresolved) trigger two possible
	// transitions:
	//
	//   in_review + all resolved + no open CHANGES → staged
	//   fixing  + all resolved + no open CHANGES → in_review
	//
	// The second transition is what closes the dev-agent fixing loop: once
	// Amelia has resolved every CR thread on GitHub, the issue lifts back
	// to in_review and is ready for CR's next review pass (which will then
	// flip it to staged via the review path).
	if in.NoOpenCRChangesRequest && in.NoUnresolvedCRThreads {
		switch in.IssueStatus {
		case StatusInReview:
			return Decision{
				Action:       ActionSetStatus,
				NewStatus:    StatusStaged,
				ActivityKind: "review_passed",
			}
		case StatusFixing:
			return Decision{
				Action:       ActionSetStatus,
				NewStatus:    StatusInReview,
				ActivityKind: "review_threads_resolved",
			}
		}
	}
	return Decision{Action: ActionNoop}
}

// isAtOrPast returns true when current is at or past target in the
// PR-driven status progression: in_review → staged → done. Anything else
// (todo/in_progress/blocked) counts as "before".
func isAtOrPast(current, target string) bool {
	rank := map[string]int{
		StatusInReview: 1,
		StatusStaged:   2,
		StatusDone:     3,
	}
	c, cok := rank[current]
	t, tok := rank[target]
	if !cok || !tok {
		return false
	}
	return c >= t
}
