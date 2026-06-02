package issueguard

import "testing"

func TestShouldEmitParentQueueGapForAI44Shape(t *testing.T) {
	children := []QueueGapChildState{
		{Status: "in_review"},
		{Status: "in_review"},
		{Status: "done"},
	}

	if !ShouldEmitParentQueueGap("in_progress", "in_review", "in_progress", []byte(`{}`), children) {
		t.Fatal("expected queue gap when active parent has no todo/in_progress children after child returns to review")
	}
}

func TestShouldEmitParentQueueGapRequiresLastActiveChild(t *testing.T) {
	children := []QueueGapChildState{
		{Status: "in_review"},
		{Status: "todo"},
	}

	if ShouldEmitParentQueueGap("in_progress", "in_review", "in_progress", []byte(`{}`), children) {
		t.Fatal("expected no queue gap while another child remains todo")
	}
}

func TestShouldEmitParentQueueGapTreatsWaitingMetadataAsExplicitWait(t *testing.T) {
	children := []QueueGapChildState{
		{Status: "in_review"},
	}

	if ShouldEmitParentQueueGap("in_progress", "in_review", "in_progress", []byte(`{"waiting_on":"owner approval"}`), children) {
		t.Fatal("expected no queue gap when parent metadata already explains what it is waiting on")
	}
}

func TestShouldEmitParentQueueGapDoesNotFireForBlockedTransition(t *testing.T) {
	children := []QueueGapChildState{
		{Status: "blocked"},
	}

	if ShouldEmitParentQueueGap("in_progress", "blocked", "in_progress", []byte(`{}`), children) {
		t.Fatal("expected no queue gap when the child explicitly moved to blocked")
	}
}
