package github

// State machine for translating GitHub PR / review events into Multica issue
// status transitions.
//
// The machine is intentionally a pure function: given an Input, it returns a
// Decision. The webhook handler is responsible for I/O (loading the issue,
// querying review state from GitHub) and for applying the Decision.
//
// Status vocabulary (already permitted by the issue.status CHECK constraint
// since migration 1000): backlog, todo, in_progress, in_review, done, blocked,
// cancelled, planning, ready_for_dev, code_review, fixing, testing, checkpoint,
// staged.
//
// Status transition table (matches the design doc agreed in CR-PR planning):
//
//   Event                                              From            To
//   ---------------------------------------------------------------- ------------
//   pull_request.opened                                any             in_review
//   pull_request.synchronize                           matched         in_review
//   review.submitted state=changes_requested (CR bot)  any             in_progress
//   review.submitted (any other CR signal) +
//     no open CHANGES_REQUESTED + no unresolved
//     CR threads                                       in_review       staged
//   review_thread (any) + same predicate above         in_review       staged
//   pull_request.closed merged=true                    any             done
//   pull_request.closed merged=false                   any             blocked
//   pull_request.reopened                              blocked|done    in_review
//
// Anything not listed above is a no-op (Decision.Action == ActionNoop).

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
	StatusStaged     = "staged"
	StatusBlocked    = "blocked"
	StatusDone       = "done"
)

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

	// Review-event fields (populated when Kind == EventKindReview).
	ReviewState ReviewState
	ReviewByCR  bool // review submitted by the configured CR bot

	// CR-thread predicate for the *current* PR state on GitHub. The handler
	// computes this by listing review threads via the GitHub API after
	// receiving any review or thread event.
	NoOpenCRChangesRequest bool // no open review with state=changes_requested from CR bot
	NoUnresolvedCRThreads  bool // zero unresolved review threads from CR bot
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
		// Agent pushed a fix after CHANGES_REQUESTED. Move back to in_review
		// so a clean CR pass can flip to staged on the next event. If we're
		// already at in_review or past it, leave alone.
		if in.IssueStatus == StatusInProgress {
			return Decision{
				Action:       ActionSetStatus,
				NewStatus:    StatusInReview,
				ActivityKind: "pr_updated",
			}
		}
		return Decision{Action: ActionNoop}

	case PRActionClosed:
		if in.Merged {
			if in.IssueStatus == StatusDone {
				return Decision{Action: ActionNoop}
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
		// Bounce back to in_progress regardless of current state, except
		// when we're already in_progress (avoid duplicate activity).
		if in.IssueStatus == StatusInProgress {
			return Decision{Action: ActionNoop}
		}
		return Decision{
			Action:       ActionSetStatus,
			NewStatus:    StatusInProgress,
			ActivityKind: "review_changes_requested",
		}
	}

	// Non-CHANGES review (approved / commented). Re-evaluate the staged
	// predicate: only flip if we're currently in_review.
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
	// Thread-level events (resolved / unresolved) only matter as a trigger
	// to re-evaluate the staged predicate.
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
