package replyadmission

import (
	"errors"
	"testing"
)

func TestCheckRequiresRequesterMentionForSubstantiveOpinionReply(t *testing.T) {
	requesterID := "11111111-1111-4111-8111-111111111111"
	err := Check(Parent{
		AuthorType: "agent",
		AuthorID:   requesterID,
		Content:    "Codex, what is your opinion on this review?",
	}, "The review is sound and the cost constraint is binding.")
	if err == nil {
		t.Fatal("expected a missing requester mention error")
	}
	var missing *MissingRequesterMentionError
	if !errors.As(err, &missing) {
		t.Fatalf("expected MissingRequesterMentionError, got %T: %v", err, err)
	}
	if missing.RequesterID != requesterID {
		t.Fatalf("requester id = %q, want %s", missing.RequesterID, requesterID)
	}
	if got := missing.Code(); got != MissingRequesterMentionCode {
		t.Fatalf("error code = %q, want %q", got, MissingRequesterMentionCode)
	}
}

func TestEvaluateReturnsStructuredAdmissionDecision(t *testing.T) {
	requesterID := "12121212-1212-4121-8121-121212121212"
	decision := Evaluate(Parent{
		ID:         "23232323-2323-4232-8232-232323232323",
		IssueID:    "34343434-3434-4434-8434-343434343434",
		AuthorType: "agent",
		AuthorID:   requesterID,
		Content:    "Could you weigh in on this proposal?",
	}, "The proposal is sound.")
	if decision.Admitted {
		t.Fatal("unmentioned substantive reply was admitted")
	}
	if decision.Classification != ClassificationSubstantive {
		t.Fatalf("classification = %q, want %q", decision.Classification, ClassificationSubstantive)
	}
	if decision.Requirement != RequirementRequesterMention {
		t.Fatalf("requirement = %q, want %q", decision.Requirement, RequirementRequesterMention)
	}
	if decision.Reason != ReasonMissingRequesterMention {
		t.Fatalf("reason = %q, want %q", decision.Reason, ReasonMissingRequesterMention)
	}
	if decision.PolicyVersion != PolicyVersion {
		t.Fatalf("policy version = %q, want %q", decision.PolicyVersion, PolicyVersion)
	}
	if decision.RequesterID != requesterID {
		t.Fatalf("requester id = %q, want %q", decision.RequesterID, requesterID)
	}
}

func TestEvaluateClassifiesAcknowledgementWithoutRequirement(t *testing.T) {
	decision := Evaluate(Parent{
		AuthorType: "agent",
		AuthorID:   "45454545-4545-4454-8454-454545454545",
		Content:    "What do you think about this?",
	}, "Acknowledged.")
	if !decision.Admitted {
		t.Fatal("acknowledgement was rejected")
	}
	if decision.Classification != ClassificationAcknowledgement {
		t.Fatalf("classification = %q, want %q", decision.Classification, ClassificationAcknowledgement)
	}
	if decision.Requirement != RequirementNone {
		t.Fatalf("requirement = %q, want %q", decision.Requirement, RequirementNone)
	}
}

func TestCheckAllowsMentionedSubstantiveOpinionReply(t *testing.T) {
	requesterID := "22222222-2222-4222-8222-222222222222"
	err := Check(Parent{
		AuthorType: "agent",
		AuthorID:   requesterID,
		Content:    "Please give your opinion on this review.",
	}, "[@Requester](mention://agent/"+requesterID+")\n\nThe review is sound.")
	if err != nil {
		t.Fatalf("mentioned substantive reply rejected: %v", err)
	}
}

func TestCheckAllowsMentionAfterInlineCode(t *testing.T) {
	requesterID := "24242424-2424-4242-8242-242424242424"
	err := Check(Parent{
		AuthorType: "agent",
		AuthorID:   requesterID,
		Content:    "Please give your opinion on this review.",
	}, "CHANGES_REQUIRED. `server/internal/handler/comment.go:1894` persists before admission.\n\n[@Requester](mention://agent/"+requesterID+")")
	if err != nil {
		t.Fatalf("mention after balanced inline code rejected: %v", err)
	}
}

func TestCheckTreatsUnbalancedInlineCodeAsLiteral(t *testing.T) {
	requesterID := "25252525-2525-4252-8252-252525252525"
	err := Check(Parent{
		AuthorType: "agent",
		AuthorID:   requesterID,
		Content:    "Please give your opinion on this review.",
	}, "The `reference has no closing delimiter.\n\n[@Requester](mention://agent/"+requesterID+")")
	if err != nil {
		t.Fatalf("mention after unbalanced inline code rejected: %v", err)
	}
}

func TestCheckNestedReplyDoesNotRequireTagBack(t *testing.T) {
	requesterID := "26262626-2626-4262-8262-262626262626"
	err := Check(Parent{
		AuthorType: "agent",
		AuthorID:   requesterID,
		Content:    "Could you review this implementation and tell me what you think?",
		IsReply:    true,
	}, "Understood, no further action needed")
	if err != nil {
		t.Fatalf("nested reply unexpectedly required a tag-back: %v", err)
	}
}

func TestCheckKeepsAcknowledgementsAndNonOpinionParentsExempt(t *testing.T) {
	tests := []struct {
		name    string
		parent  Parent
		content string
	}{
		{
			name: "acknowledgement",
			parent: Parent{
				AuthorType: "agent",
				AuthorID:   "33333333-3333-4333-8333-333333333333",
				Content:    "What do you think about this?",
			},
			content: "Acknowledged.",
		},
		{
			name: "member parent",
			parent: Parent{
				AuthorType: "member",
				AuthorID:   "member-requester",
				Content:    "What is your opinion?",
			},
			content: "The review is sound.",
		},
		{
			name: "non-opinion agent parent",
			parent: Parent{
				AuthorType: "agent",
				AuthorID:   "44444444-4444-4444-8444-444444444444",
				Content:    "I have started the implementation.",
			},
			content: "The implementation is complete.",
		},
		{
			name: "review statement is not a request",
			parent: Parent{
				AuthorType: "agent",
				AuthorID:   "66666666-6666-4666-8666-666666666666",
				Content:    "The review is sound.",
			},
			content: "The cost constraint is binding.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Check(tt.parent, tt.content); err != nil {
				t.Fatalf("reply unexpectedly rejected: %v", err)
			}
		})
	}
}

func TestCheckRequiresCanonicalMentionNotDisplayNameText(t *testing.T) {
	requesterID := "55555555-5555-4555-8555-555555555555"
	err := Check(Parent{
		AuthorType: "agent",
		AuthorID:   requesterID,
		Content:    "Do you agree with this proposal?",
	}, "@Requester, I agree with the proposal.")
	if err == nil {
		t.Fatal("expected a canonical mention to be required")
	}
}

func TestCheckRecognizesOpinionRequestVariants(t *testing.T) {
	requesterID := "77777777-7777-4777-8777-777777777777"
	for _, parentContent := range []string{
		"Could you weigh in on this?",
		"What's your take on this?",
		"Please assess this.",
		"Could you critique this?",
		"What is your view on this?",
		"Would you recommend this?",
		"Can you advise on this?",
		"How do you feel about this?",
		"What would you do here?",
		"Would you choose option A?",
		"Review this?",
		"Review this and let me know what you think.",
		"Please review this.",
	} {
		t.Run(parentContent, func(t *testing.T) {
			if err := Check(Parent{AuthorType: "agent", AuthorID: requesterID, Content: parentContent}, "The proposal is sound."); err == nil {
				t.Fatal("expected requester mention to be required")
			}
		})
	}
}

func TestCheckDoesNotGateFactualReviewStatusQuestion(t *testing.T) {
	requesterID := "88888888-8888-4888-8888-888888888888"
	if err := Check(Parent{
		AuthorType: "agent",
		AuthorID:   requesterID,
		Content:    "Is the review complete?",
	}, "Yes, the review is complete."); err != nil {
		t.Fatalf("factual status reply unexpectedly rejected: %v", err)
	}
}

func TestCheckDoesNotGateFactualConfirmationOrOwnershipQuestion(t *testing.T) {
	requesterID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, parentContent := range []string{
		"Can you confirm the sound level?",
		"Do you take ownership of this task?",
	} {
		t.Run(parentContent, func(t *testing.T) {
			if err := Check(Parent{AuthorType: "agent", AuthorID: requesterID, Content: parentContent}, "Yes, confirmed."); err != nil {
				t.Fatalf("factual reply unexpectedly rejected: %v", err)
			}
		})
	}
}

func TestCheckIgnoresCodeFormattedMention(t *testing.T) {
	requesterID := "99999999-9999-4999-8999-999999999999"
	parent := Parent{
		AuthorType: "agent",
		AuthorID:   requesterID,
		Content:    "What do you think about this?",
	}
	for _, response := range []string{
		"`[@Requester](mention://agent/" + requesterID + ")`\n\nThe proposal is sound.",
		"```md\n[@Requester](mention://agent/" + requesterID + ")\n```\nThe proposal is sound.",
		"~~~md\n[@Requester](mention://agent/" + requesterID + ")\n~~~\nThe proposal is sound.",
		"    [@Requester](mention://agent/" + requesterID + ")\nThe proposal is sound.",
	} {
		t.Run(response, func(t *testing.T) {
			if err := Check(parent, response); err == nil {
				t.Fatal("code-formatted mention incorrectly satisfied admission")
			}
		})
	}
}

func TestFingerprintIncludesPostCommitSideEffectInputs(t *testing.T) {
	base := RequestFingerprint{
		IssueID:     "issue",
		WorkspaceID: "workspace",
		AuthorType:  "agent",
		AuthorID:    "agent",
		Content:     "content",
		Type:        "comment",
	}
	withAttachment := base
	withAttachment.AttachmentIDs = []string{"attachment-1"}
	withSuppression := base
	withSuppression.SuppressAgentIDs = []string{"agent-2"}

	if Fingerprint(base) == Fingerprint(withAttachment) {
		t.Fatal("attachment inputs are not part of the idempotency fingerprint")
	}
	if Fingerprint(base) == Fingerprint(withSuppression) {
		t.Fatal("suppression inputs are not part of the idempotency fingerprint")
	}
}
