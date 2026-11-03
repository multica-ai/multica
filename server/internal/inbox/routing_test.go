package inbox

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestRoute(t *testing.T) {
	muted := ""

	cases := []struct {
		name      string
		prefs     map[string]string
		notifType string
		want      string // muted means not delivered
	}{
		// Defaults match DefaultSection, so an untouched account sees no change.
		{"default assigned", nil, "issue_assigned", SectionAction},
		{"default mention", nil, "mentioned", SectionAction},
		{"default comment without audience", nil, "new_comment", SectionAction},
		{"default agent failure", nil, "task_failed", SectionAction},
		{"default status", nil, "status_changed", SectionActivity},
		{"default assignee change", nil, "unassigned", SectionActivity},
		{"default update", nil, "due_date_changed", SectionActivity},
		{"explicit all is the default", map[string]string{"status_changes": "all"}, "status_changed", SectionActivity},

		// The recipient's choice wins.
		{"status routed to action", map[string]string{"status_changes": "action"}, "status_changed", SectionAction},
		{"mentions routed to activity", map[string]string{"mentions": "activity"}, "mentioned", SectionActivity},
		{"updates muted", map[string]string{"updates": "muted"}, "priority_changed", muted},

		// Muting comment volume never silences someone asking for you by name.
		{"muted comments still delivers mentions", map[string]string{"comments": "muted"}, "mentioned", SectionAction},
		{"muted mentions drops mentioned", map[string]string{"mentions": "muted"}, "mentioned", muted},
		{"muted mentions still delivers new_comment", map[string]string{"mentions": "muted"}, "new_comment", SectionAction},

		// A muted pre-routing group mutes both halves until the new key is set.
		{"legacy comments muted drops comment", map[string]string{"comments": "muted"}, "new_comment", muted},
		{"legacy assignments muted drops assigned", map[string]string{"assignments": "muted"}, "issue_assigned", muted},
		{"legacy assignments muted drops unassigned", map[string]string{"assignments": "muted"}, "unassigned", muted},
		{"legacy agent_activity muted drops failure", map[string]string{"agent_activity": "muted"}, "task_failed", muted},
		{"muting comments leaves assignments alone", map[string]string{"comments": "muted"}, "issue_assigned", SectionAction},

		// Types outside the routable kinds always land in their default section.
		{"unroutable reaction", map[string]string{"comments": "muted"}, "reaction_added", SectionActivity},
		{"unroutable review request", map[string]string{"assigned": "muted"}, "review_requested", SectionAction},
		{"unknown value falls back to default", map[string]string{"status_changes": "loud"}, "status_changed", SectionActivity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertRoute(t, c.want, muted)(Route(c.prefs, c.notifType))
		})
	}
}

func TestRouteComment(t *testing.T) {
	muted := ""

	cases := []struct {
		name     string
		prefs    map[string]string
		audience CommentAudience
		want     string // muted means not delivered
	}{
		// Defaults: only a comment written to someone else is activity.
		{"default to nobody", nil, CommentToNobody, SectionAction},
		{"default to recipient", nil, CommentToRecipient, SectionAction},
		{"default to others", nil, CommentToOthers, SectionActivity},

		// Each audience follows its own key.
		{"to nobody routed to activity", map[string]string{"comments_without_mentions": "activity"}, CommentToNobody, SectionActivity},
		{"to others routed to action", map[string]string{"comments_mentioning_others": "action"}, CommentToOthers, SectionAction},
		{"to others muted", map[string]string{"comments_mentioning_others": "muted"}, CommentToOthers, muted},
		{"to others choice leaves to nobody alone", map[string]string{"comments_mentioning_others": "muted"}, CommentToNobody, SectionAction},
		{"to nobody choice leaves to others alone", map[string]string{"comments_without_mentions": "muted"}, CommentToOthers, SectionActivity},

		// A comment that @mentions the recipient follows the mentions choice,
		// so it lands with the "mentioned" notification it also sends.
		{"to recipient follows mentions", map[string]string{"mentions": "activity"}, CommentToRecipient, SectionActivity},
		{"to recipient muted with mentions", map[string]string{"mentions": "muted"}, CommentToRecipient, muted},
		{"to recipient ignores comment keys", map[string]string{"comments_without_mentions": "muted", "comments_mentioning_others": "muted"}, CommentToRecipient, SectionAction},

		// A muted pre-routing comments group still mutes every comment until
		// the recipient sets the new key.
		{"legacy comments muted drops to nobody", map[string]string{"comments": "muted"}, CommentToNobody, muted},
		{"legacy comments muted drops to others", map[string]string{"comments": "muted"}, CommentToOthers, muted},
		{"legacy comments muted drops to recipient", map[string]string{"comments": "muted"}, CommentToRecipient, muted},
		{"new key overrides muted legacy group", map[string]string{"comments": "muted", "comments_mentioning_others": "activity"}, CommentToOthers, SectionActivity},
		{"mentions key overrides muted legacy group", map[string]string{"comments": "muted", "mentions": "action"}, CommentToRecipient, SectionAction},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertRoute(t, c.want, muted)(RouteComment(c.prefs, c.audience))
		})
	}
}

func assertRoute(t *testing.T, want, muted string) func(string, bool) {
	t.Helper()
	return func(got string, ok bool) {
		t.Helper()
		if want == muted {
			if ok {
				t.Fatalf("route = (%q, delivered), want muted", got)
			}
			return
		}
		if !ok || got != want {
			t.Fatalf("route = (%q, %v), want (%q, delivered)", got, ok, want)
		}
	}
}

func TestRouteKeysAreRouteKinds(t *testing.T) {
	for _, key := range []string{RouteKeyMentions, RouteKeyCommentsWithoutMentions, RouteKeyCommentsMentioningOthers} {
		if !IsRouteKey(key) {
			t.Errorf("%q is not a route kind", key)
		}
	}
	for _, retired := range []string{"member_comments", "agent_comments"} {
		if IsRouteKey(retired) {
			t.Errorf("%q should no longer be a route kind", retired)
		}
	}
}

func TestItemSection(t *testing.T) {
	if got := ItemSection(SectionText(SectionAction), "status_changed"); got != SectionAction {
		t.Errorf("stored action = %q", got)
	}
	if got := ItemSection(SectionText(SectionActivity), "new_comment"); got != SectionActivity {
		t.Errorf("stored activity = %q", got)
	}
	if got := ItemSection(pgtype.Text{}, "status_changed"); got != SectionActivity {
		t.Errorf("unset falls back to default, got %q", got)
	}
	if got := ItemSection(SectionText("bogus"), "mentioned"); got != SectionAction {
		t.Errorf("unknown stored value falls back to default, got %q", got)
	}
}
