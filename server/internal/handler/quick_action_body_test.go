package handler

import (
	"encoding/json"
	"os"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestBuildQuickActionBodySharedContract(t *testing.T) {
	// The frontend consumes the same cases to keep draft and run text aligned.
	data, err := os.ReadFile("testdata/quick_action_bodies.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string `json:"name"`
		Action struct {
			AssigneeType string `json:"assignee_type"`
			AssigneeID   string `json:"assignee_id"`
			TargetName   string `json:"target_name"`
			Prompt       string `json:"prompt"`
		} `json:"action"`
		Expected string `json:"expected"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("shared quick action body cases must not be empty")
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := buildQuickActionBody(
				db.QuickAction{AssigneeType: tc.Action.AssigneeType, Prompt: tc.Action.Prompt},
				quickActionTarget{Name: tc.Action.TargetName, MentionType: tc.Action.AssigneeType, MentionID: tc.Action.AssigneeID},
			)
			if got != tc.Expected {
				t.Fatalf("body = %q, want %q", got, tc.Expected)
			}
		})
	}
}
