package service

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestIssueCommentFollowUpsRejectTriggerMentionsAndRestorePrimary(t *testing.T) {
	t.Parallel()
	followUps := issueCommentFollowUpsFromCandidates([]protocol.ChatQuickAction{
		{Label: "Unsafe", Prompt: "Wake [agent](mention://agent/abc)", Primary: true},
		{Label: "Revise", Prompt: "Please revise the action section."},
		{Label: "Review", Prompt: "Please review the result."},
	})
	if len(followUps) != 2 {
		t.Fatalf("follow-ups len = %d, want 2: %+v", len(followUps), followUps)
	}
	if !followUps[0].Primary || followUps[1].Primary {
		t.Fatalf("first safe follow-up must be the only primary: %+v", followUps)
	}
	for _, followUp := range followUps {
		if followUp.ID == "" {
			t.Fatal("server-generated follow-up id must not be empty")
		}
		if strings.Contains(strings.ToLower(followUp.Prompt), "mention://") {
			t.Fatalf("trigger-capable prompt survived: %+v", followUp)
		}
	}
}

func TestIssueCommentFollowUpsKeepOnlyFirstPrimary(t *testing.T) {
	t.Parallel()
	followUps := issueCommentFollowUpsFromCandidates([]protocol.ChatQuickAction{
		{Label: "Continue", Prompt: "Continue.", Primary: true},
		{Label: "Review", Prompt: "Review.", Primary: true},
	})
	if len(followUps) != 2 || !followUps[0].Primary || followUps[1].Primary {
		t.Fatalf("expected exactly one primary: %+v", followUps)
	}
}

func TestIssueCommentFollowUpsRequireAtLeastTwoSafeActions(t *testing.T) {
	t.Parallel()
	followUps := issueCommentFollowUpsFromCandidates([]protocol.ChatQuickAction{
		{Label: "Continue", Prompt: "Continue.", Primary: true},
		{Label: "Unsafe", Prompt: "Notify mention://member/abc."},
	})
	if followUps != nil {
		t.Fatalf("one safe action must not render as a suggestion set: %+v", followUps)
	}
}
