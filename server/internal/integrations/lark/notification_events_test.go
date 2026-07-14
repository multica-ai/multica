package lark

import (
	"reflect"
	"testing"
)

func TestValidateNotificationEventTypes(t *testing.T) {
	t.Run("catalog and defaults have stable order", func(t *testing.T) {
		wantSupported := []string{
			"issue_assigned", "unassigned", "assignee_changed",
			"mentioned", "new_comment", "reaction_added",
			"status_changed", "priority_changed", "start_date_changed", "due_date_changed",
			"task_failed",
			"quick_create_failed", "quick_create_done",
			"autopilot_paused", "issue_subscribed",
		}
		wantDefaults := []string{
			"issue_assigned", "mentioned", "task_failed", "quick_create_failed", "autopilot_paused",
		}
		if !reflect.DeepEqual(SupportedNotificationEvents(), wantSupported) {
			t.Fatalf("supported event types = %v, want %v", SupportedNotificationEvents(), wantSupported)
		}
		if !reflect.DeepEqual(DefaultNotificationEvents(), wantDefaults) {
			t.Fatalf("default event types = %v, want %v", DefaultNotificationEvents(), wantDefaults)
		}
	})

	t.Run("catalog accessors return independent copies", func(t *testing.T) {
		supported := SupportedNotificationEvents()
		defaults := DefaultNotificationEvents()
		supported[0] = "mutated"
		defaults[0] = "mutated"

		if got := SupportedNotificationEvents()[0]; got != "issue_assigned" {
			t.Fatalf("supported catalog was mutated through returned slice: %q", got)
		}
		if got := DefaultNotificationEvents()[0]; got != "issue_assigned" {
			t.Fatalf("default catalog was mutated through returned slice: %q", got)
		}
		validated, err := ValidateNotificationEventTypes([]string{"issue_assigned"})
		if err != nil || !reflect.DeepEqual(validated, []string{"issue_assigned"}) {
			t.Fatalf("validator observed caller mutation: result=%v err=%v", validated, err)
		}
	})

	t.Run("deduplicates and returns catalog order", func(t *testing.T) {
		got, err := ValidateNotificationEventTypes([]string{
			"autopilot_paused", "mentioned", "issue_assigned", "mentioned",
		})
		if err != nil {
			t.Fatalf("ValidateNotificationEventTypes returned error: %v", err)
		}
		want := []string{"issue_assigned", "mentioned", "autopilot_paused"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("validated event types = %v, want %v", got, want)
		}
	})

	t.Run("rejects unknown event type", func(t *testing.T) {
		if _, err := ValidateNotificationEventTypes([]string{"mentioned", "unknown_event"}); err == nil {
			t.Fatal("expected unknown event type error")
		}
	})

	t.Run("empty input returns non-nil empty slice", func(t *testing.T) {
		got, err := ValidateNotificationEventTypes([]string{})
		if err != nil {
			t.Fatalf("ValidateNotificationEventTypes returned error: %v", err)
		}
		if got == nil {
			t.Fatal("validated event types must be non-nil")
		}
		if len(got) != 0 {
			t.Fatalf("validated event types = %v, want empty", got)
		}
	})
}
