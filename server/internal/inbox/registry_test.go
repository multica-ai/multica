package inbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// The web, desktop and mobile clients switch on InboxItemType. It must list
// exactly the types the server delivers plus the legacy types older releases
// wrote: a delivered type missing there is a row no client can render, a
// legacy type missing there is history that loses its label and filter, and a
// type the server knows neither way is a client branch for a row that is
// never written. This file is why packages/core/types/inbox.ts belongs in
// CI's backend path filter.
func TestRegistryMatchesFrontendTypes(t *testing.T) {
	path := filepath.Join("..", "..", "..", "packages", "core", "types", "inbox.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	frontend, err := parseInboxItemType(string(src))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	for _, problem := range frontendTypeMismatches(frontend) {
		t.Error(problem)
	}
}

func TestLegacyTypesAreNotDelivered(t *testing.T) {
	for _, legacy := range legacyTypes {
		if _, ok := Group(legacy); ok {
			t.Errorf("%q is both delivered and legacy", legacy)
		}
	}
}

// The union's comments may contain semicolons, as the legacy note in
// inbox.ts does; the declaration ends only at one outside a comment.
func TestParseInboxItemTypeSkipsComments(t *testing.T) {
	src := `export type InboxSeverity = "info";

export type InboxItemType =
  | "issue_assigned"
  // No longer written, but older servers wrote them and those rows remain;
  // keep their labels and filter groups so history still reads and filters.
  | "review_requested"
  /* a block; with "quotes" */
  | "quick_create_done";

export interface InboxItem { type: "not_a_member" }
`
	got, err := parseInboxItemType(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"issue_assigned", "quick_create_done", "review_requested"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("parsed %v, want %v", got, want)
	}
}

func TestFrontendTypeMismatches(t *testing.T) {
	complete := append(Types(), legacyTypes...)
	without := func(drop string) []string {
		var out []string
		for _, v := range complete {
			if v != drop {
				out = append(out, v)
			}
		}
		return out
	}
	cases := []struct {
		name     string
		frontend []string
		want     string
	}{
		{"delivered plus legacy matches", complete, ""},
		{"dropping a delivered type fails", without("children_done"), `"children_done"`},
		{"dropping a legacy type fails", without("agent_blocked"), `"agent_blocked"`},
		{"an unknown type fails", append(append([]string(nil), complete...), "brand_new"), `"brand_new"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := strings.Join(frontendTypeMismatches(tc.frontend), "\n")
			if tc.want == "" && problems != "" {
				t.Fatalf("unexpected mismatches:\n%s", problems)
			}
			if !strings.Contains(problems, tc.want) {
				t.Fatalf("mismatches %q do not name %s", problems, tc.want)
			}
		})
	}
}

// parseInboxItemType returns the string literals of the InboxItemType union,
// sorted. It skips // and /* */ comments, so a semicolon or quote inside one
// neither ends the declaration nor adds a member.
func parseInboxItemType(src string) ([]string, error) {
	const decl = "export type InboxItemType"
	start := strings.Index(src, decl)
	if start < 0 {
		return nil, errors.New("InboxItemType not found")
	}
	var types []string
	for i := start + len(decl); i < len(src); i++ {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				return nil, errors.New("InboxItemType is not terminated")
			}
			i += end
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, errors.New("unterminated block comment in InboxItemType")
			}
			i += end + 3
		case src[i] == '"':
			end := strings.IndexByte(src[i+1:], '"')
			if end < 0 {
				return nil, errors.New("unterminated string in InboxItemType")
			}
			types = append(types, src[i+1:i+1+end])
			i += end + 1
		case src[i] == ';':
			sort.Strings(types)
			return types, nil
		}
	}
	return nil, errors.New("InboxItemType is not terminated")
}

// frontendTypeMismatches compares the client's InboxItemType with the
// delivered and legacy types and describes every difference.
func frontendTypeMismatches(frontend []string) []string {
	listed := map[string]bool{}
	for _, v := range frontend {
		listed[v] = true
	}
	var problems []string
	for _, v := range Types() {
		if !listed[v] {
			problems = append(problems, fmt.Sprintf("server delivers %q but InboxItemType does not list it", v))
		}
		delete(listed, v)
	}
	for _, v := range legacyTypes {
		if !listed[v] {
			problems = append(problems, fmt.Sprintf("legacy type %q left InboxItemType; older rows would lose their label and filter", v))
		}
		delete(listed, v)
	}
	var unknown []string
	for v := range listed {
		unknown = append(unknown, v)
	}
	sort.Strings(unknown)
	for _, v := range unknown {
		problems = append(problems, fmt.Sprintf("InboxItemType lists %q but the server neither delivers it nor lists it as legacy", v))
	}
	return problems
}
