package inbox

import (
	"encoding/json"
	"testing"
)

func TestAutoArchiveEventPayloadCompatibility(t *testing.T) {
	// v0.12.0 clients (packages/core/inbox/auto-archive.ts) only treat an
	// inbox:batch-archived event as user-caused when reason is in this set.
	legacyV0120Reasons := map[string]bool{
		"issue_status_terminal": true,
		"actor_acted":           true,
	}

	cases := []struct {
		name        string
		archivedOn  []string
		wantReason  string
		wantTrigger string
	}{
		{
			name:        "completed issue uses terminal reason without trigger",
			archivedOn:  []string{AutoArchiveCompletedPref},
			wantReason:  ArchiveReasonCompleted,
			wantTrigger: "",
		},
		{
			name:        "completed takes precedence over agent trigger",
			archivedOn:  []string{AutoArchiveAgentTriggerPref, AutoArchiveCompletedPref},
			wantReason:  ArchiveReasonCompleted,
			wantTrigger: "",
		},
		{
			name:        "agent trigger preserves actor_acted reason and sets optional trigger",
			archivedOn:  []string{AutoArchiveAgentTriggerPref},
			wantReason:  ArchiveReasonActed,
			wantTrigger: ArchiveTriggerAgent,
		},
		{
			name:        "agent trigger alongside custom action preserves actor_acted and sets trigger",
			archivedOn:  []string{AutoArchiveCommentPref, AutoArchiveAgentTriggerPref},
			wantReason:  ArchiveReasonActed,
			wantTrigger: ArchiveTriggerAgent,
		},
		{
			name:        "other handled edit uses actor_acted without trigger",
			archivedOn:  []string{AutoArchiveCommentPref},
			wantReason:  ArchiveReasonActed,
			wantTrigger: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, trigger := autoArchiveEventDetails(tc.archivedOn)
			if reason != tc.wantReason || trigger != tc.wantTrigger {
				t.Fatalf("autoArchiveEventDetails(%v) = (%q, %q), want (%q, %q)",
					tc.archivedOn, reason, trigger, tc.wantReason, tc.wantTrigger)
			}

			payload := buildAutoArchivePayload("member-1", "issue-1", 2, reason, trigger)
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}

			// Verify wire JSON seen by v0.12.0 clients (which only read issue_id + reason).
			var legacy struct {
				IssueID string `json:"issue_id"`
				Reason  string `json:"reason"`
			}
			if err := json.Unmarshal(raw, &legacy); err != nil {
				t.Fatalf("unmarshal legacy view: %v", err)
			}
			if legacy.IssueID != "issue-1" {
				t.Fatalf("legacy issue_id = %q, want %q", legacy.IssueID, "issue-1")
			}
			if !legacyV0120Reasons[legacy.Reason] {
				t.Fatalf("wire reason %q breaks v0.12.0 clients; allowed set is %v", legacy.Reason, legacyV0120Reasons)
			}

			// Verify optional trigger presence on the wire.
			var wire map[string]any
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatalf("unmarshal wire map: %v", err)
			}
			gotTrigger, hasTrigger := wire["trigger"]
			if tc.wantTrigger == "" {
				if hasTrigger {
					t.Fatalf("expected trigger to be omitted, got %v", gotTrigger)
				}
			} else {
				if !hasTrigger || gotTrigger != tc.wantTrigger {
					t.Fatalf("wire trigger = %v (present=%v), want %q", gotTrigger, hasTrigger, tc.wantTrigger)
				}
			}
		})
	}
}
