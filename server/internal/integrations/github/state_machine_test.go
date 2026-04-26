package github

import "testing"

func TestDecide_PROpened(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   Decision
	}{
		{
			name:   "from todo links + sets in_review",
			status: "todo",
			want:   Decision{Action: ActionLinkPR, NewStatus: StatusInReview, ActivityKind: "pr_opened"},
		},
		{
			name:   "from in_progress links + sets in_review",
			status: "in_progress",
			want:   Decision{Action: ActionLinkPR, NewStatus: StatusInReview, ActivityKind: "pr_opened"},
		},
		{
			name:   "from staged preserves status",
			status: StatusStaged,
			want:   Decision{Action: ActionLinkPR, NewStatus: StatusStaged, ActivityKind: "pr_opened"},
		},
		{
			name:   "from done preserves status",
			status: StatusDone,
			want:   Decision{Action: ActionLinkPR, NewStatus: StatusDone, ActivityKind: "pr_opened"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(Input{
				Kind:        EventKindPR,
				IssueStatus: tc.status,
				PRAction:    PRActionOpened,
			})
			if got != tc.want {
				t.Fatalf("got %+v; want %+v", got, tc.want)
			}
		})
	}
}

func TestDecide_PRSynchronize(t *testing.T) {
	t.Run("from in_progress flips to in_review", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindPR, IssueStatus: StatusInProgress, PRAction: PRActionSynchronize,
		})
		want := Decision{Action: ActionSetStatus, NewStatus: StatusInReview, ActivityKind: "pr_updated"}
		if got != want {
			t.Fatalf("got %+v; want %+v", got, want)
		}
	})
	t.Run("from in_review is a noop", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindPR, IssueStatus: StatusInReview, PRAction: PRActionSynchronize,
		})
		if got.Action != ActionNoop {
			t.Fatalf("got %+v; want noop", got)
		}
	})
}

func TestDecide_PRClosed(t *testing.T) {
	t.Run("merged flips to done", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindPR, IssueStatus: StatusStaged, PRAction: PRActionClosed, Merged: true,
		})
		want := Decision{Action: ActionSetStatus, NewStatus: StatusDone, ActivityKind: "pr_merged"}
		if got != want {
			t.Fatalf("got %+v; want %+v", got, want)
		}
	})
	t.Run("merged from done is noop", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindPR, IssueStatus: StatusDone, PRAction: PRActionClosed, Merged: true,
		})
		if got.Action != ActionNoop {
			t.Fatalf("got %+v; want noop", got)
		}
	})
	t.Run("unmerged flips to blocked", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindPR, IssueStatus: StatusInReview, PRAction: PRActionClosed, Merged: false,
		})
		want := Decision{Action: ActionSetStatus, NewStatus: StatusBlocked, ActivityKind: "pr_closed_unmerged"}
		if got != want {
			t.Fatalf("got %+v; want %+v", got, want)
		}
	})
	t.Run("unmerged from blocked is noop", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindPR, IssueStatus: StatusBlocked, PRAction: PRActionClosed, Merged: false,
		})
		if got.Action != ActionNoop {
			t.Fatalf("got %+v; want noop", got)
		}
	})
}

func TestDecide_PRReopened(t *testing.T) {
	t.Run("from blocked → in_review", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindPR, IssueStatus: StatusBlocked, PRAction: PRActionReopened,
		})
		want := Decision{Action: ActionSetStatus, NewStatus: StatusInReview, ActivityKind: "pr_reopened"}
		if got != want {
			t.Fatalf("got %+v; want %+v", got, want)
		}
	})
	t.Run("from done → in_review", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindPR, IssueStatus: StatusDone, PRAction: PRActionReopened,
		})
		want := Decision{Action: ActionSetStatus, NewStatus: StatusInReview, ActivityKind: "pr_reopened"}
		if got != want {
			t.Fatalf("got %+v; want %+v", got, want)
		}
	})
	t.Run("from in_review is noop", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindPR, IssueStatus: StatusInReview, PRAction: PRActionReopened,
		})
		if got.Action != ActionNoop {
			t.Fatalf("got %+v; want noop", got)
		}
	})
}

func TestDecide_ReviewChangesRequested(t *testing.T) {
	t.Run("from in_review by CR → in_progress", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindReview, IssueStatus: StatusInReview,
			ReviewState: ReviewChangesRequested, ReviewByCR: true,
		})
		want := Decision{Action: ActionSetStatus, NewStatus: StatusInProgress, ActivityKind: "review_changes_requested"}
		if got != want {
			t.Fatalf("got %+v; want %+v", got, want)
		}
	})
	t.Run("from staged by CR → in_progress (re-bounce)", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindReview, IssueStatus: StatusStaged,
			ReviewState: ReviewChangesRequested, ReviewByCR: true,
		})
		if got.NewStatus != StatusInProgress {
			t.Fatalf("got %+v; want NewStatus=in_progress", got)
		}
	})
	t.Run("non-CR reviewer is ignored", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindReview, IssueStatus: StatusInReview,
			ReviewState: ReviewChangesRequested, ReviewByCR: false,
		})
		if got.Action != ActionNoop {
			t.Fatalf("got %+v; want noop", got)
		}
	})
	t.Run("already in_progress is noop", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindReview, IssueStatus: StatusInProgress,
			ReviewState: ReviewChangesRequested, ReviewByCR: true,
		})
		if got.Action != ActionNoop {
			t.Fatalf("got %+v; want noop", got)
		}
	})
}

func TestDecide_ReviewClean(t *testing.T) {
	t.Run("clean first pass from in_review → staged", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindReview, IssueStatus: StatusInReview,
			ReviewState: ReviewCommented, ReviewByCR: true,
			NoOpenCRChangesRequest: true, NoUnresolvedCRThreads: true,
		})
		want := Decision{Action: ActionSetStatus, NewStatus: StatusStaged, ActivityKind: "review_passed"}
		if got != want {
			t.Fatalf("got %+v; want %+v", got, want)
		}
	})
	t.Run("approved review with predicate → staged", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindReview, IssueStatus: StatusInReview,
			ReviewState: ReviewApproved, ReviewByCR: true,
			NoOpenCRChangesRequest: true, NoUnresolvedCRThreads: true,
		})
		if got.NewStatus != StatusStaged {
			t.Fatalf("got %+v; want NewStatus=staged", got)
		}
	})
	t.Run("predicate fails → noop", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindReview, IssueStatus: StatusInReview,
			ReviewState: ReviewCommented, ReviewByCR: true,
			NoOpenCRChangesRequest: false, NoUnresolvedCRThreads: true,
		})
		if got.Action != ActionNoop {
			t.Fatalf("got %+v; want noop", got)
		}
	})
	t.Run("from in_progress (post-bounce) is noop until synchronize", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindReview, IssueStatus: StatusInProgress,
			ReviewState: ReviewCommented, ReviewByCR: true,
			NoOpenCRChangesRequest: true, NoUnresolvedCRThreads: true,
		})
		if got.Action != ActionNoop {
			t.Fatalf("got %+v; want noop", got)
		}
	})
}

func TestDecide_ReviewThread(t *testing.T) {
	t.Run("thread resolved + predicate → staged", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindReviewThread, IssueStatus: StatusInReview,
			NoOpenCRChangesRequest: true, NoUnresolvedCRThreads: true,
		})
		want := Decision{Action: ActionSetStatus, NewStatus: StatusStaged, ActivityKind: "review_passed"}
		if got != want {
			t.Fatalf("got %+v; want %+v", got, want)
		}
	})
	t.Run("thread resolved but still has unresolved → noop", func(t *testing.T) {
		got := Decide(Input{
			Kind: EventKindReviewThread, IssueStatus: StatusInReview,
			NoOpenCRChangesRequest: true, NoUnresolvedCRThreads: false,
		})
		if got.Action != ActionNoop {
			t.Fatalf("got %+v; want noop", got)
		}
	})
}
