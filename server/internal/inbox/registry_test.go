package inbox

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestMutedGroupSplit pins the contract the `mentions` group exists for:
// muting comment VOLUME must never silence someone asking for you BY NAME.
// `new_comment` and `mentioned` shared the `comments` group until #6468, so a
// user who muted comment churn also lost every mention — and a muted item is
// never created, so it was unrecoverable rather than merely delayed.
func TestMutedGroupSplit(t *testing.T) {
	cases := []struct {
		name      string
		prefs     map[string]string
		notifType string
		want      bool
	}{
		// The regression this split fixes.
		{"muted comments still delivers mentions", map[string]string{"comments": "muted"}, "mentioned", false},
		{"muted comments drops new_comment", map[string]string{"comments": "muted"}, "new_comment", true},
		{"muted mentions drops mentioned", map[string]string{"mentions": "muted"}, "mentioned", true},
		{"muted mentions still delivers new_comment", map[string]string{"mentions": "muted"}, "new_comment", false},
		{"both muted drops both", map[string]string{"comments": "muted", "mentions": "muted"}, "mentioned", true},

		// Absent key means the default "all" — preferences are stored sparse,
		// which is why existing `{"comments":"muted"}` rows need no migration.
		{"empty prefs delivers mentions", map[string]string{}, "mentioned", false},
		{"empty prefs delivers comments", map[string]string{}, "new_comment", false},
		{"explicit all delivers", map[string]string{"comments": "all"}, "new_comment", false},

		// Other groups are unaffected by the split.
		{"assignments group intact", map[string]string{"assignments": "muted"}, "issue_assigned", true},
		{"status group intact", map[string]string{"status_changes": "muted"}, "status_changed", true},
		{"muting comments leaves assignments alone", map[string]string{"comments": "muted"}, "issue_assigned", false},

		// Reactions have their own group, apart from comments.
		{"muted reactions drops reaction_added", map[string]string{"reactions": "muted"}, "reaction_added", true},
		{"muted comments still delivers reactions", map[string]string{"comments": "muted"}, "reaction_added", false},

		// Types outside the map are always delivered.
		{"unconfigurable type always delivered", map[string]string{"comments": "muted"}, "children_done", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Muted(tc.prefs, tc.notifType); got != tc.want {
				t.Fatalf("Muted(%v, %q) = %v, want %v",
					tc.prefs, tc.notifType, got, tc.want)
			}
		})
	}
}

// App results follow the agent-activity group; the requests that stop an
// agent until the person answers can never be muted.
func TestConnectedAppTypeGroups(t *testing.T) {
	muted := map[string]string{GroupAgentActivity: "muted"}
	if !Muted(muted, "app_action_result") {
		t.Error("app_action_result must follow the agent_activity mute")
	}
	for _, notifType := range []string{"app_action_requested", "app_action_reminder", "app_connect_requested"} {
		if Muted(muted, notifType) {
			t.Errorf("%s must always be delivered", notifType)
		}
	}
}

// The web, desktop and mobile clients switch on InboxItemType, and the server
// writes only registered types. A type added or dropped on one side alone is
// either a row no client can render or a client branch for a row that is never
// written. This file is why packages/core/types/inbox.ts belongs in CI's
// backend path filter.
func TestRegistryMatchesFrontendTypes(t *testing.T) {
	path := filepath.Join("..", "..", "..", "packages", "core", "types", "inbox.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	union := regexp.MustCompile(`(?s)export type InboxItemType\s*=(.*?);`).FindSubmatch(src)
	if union == nil {
		t.Fatalf("InboxItemType not found in %s", path)
	}
	body := regexp.MustCompile(`(?m)//.*$`).ReplaceAll(union[1], nil)
	var frontend []string
	for _, m := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(body, -1) {
		frontend = append(frontend, string(m[1]))
	}
	sort.Strings(frontend)

	server := Types()
	if strings.Join(frontend, ",") == strings.Join(server, ",") {
		return
	}
	inFrontend := map[string]bool{}
	for _, v := range frontend {
		inFrontend[v] = true
	}
	for _, v := range server {
		if !inFrontend[v] {
			t.Errorf("server registers %q but InboxItemType does not list it", v)
		}
		delete(inFrontend, v)
	}
	for v := range inFrontend {
		t.Errorf("InboxItemType lists %q but the server never registers it", v)
	}
}
