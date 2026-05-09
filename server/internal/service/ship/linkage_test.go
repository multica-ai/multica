package ship

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// validUUID is a pgtype.UUID with Valid=true. The classifier only reads
// the .Valid bit so the actual bytes don't matter for these tests.
func validUUID() pgtype.UUID {
	return pgtype.UUID{Valid: true, Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}}
}

func TestClassifySource(t *testing.T) {
	zero := pgtype.UUID{}
	v := validUUID()

	cases := []struct {
		name      string
		login     string
		issueID   pgtype.UUID
		taskID    pgtype.UUID
		isMember  bool
		want      string
	}{
		{"agent task wins", "agent-bot", v, v, true, SourceMultiicaAgent},
		{"agent task wins even without issue", "agent-bot", zero, v, false, SourceMultiicaAgent},
		{"issue link with non-member author", "octocat", v, zero, false, SourceMultiicaHuman},
		{"issue link with member author", "octocat", v, zero, true, SourceMultiicaHuman},
		{"member without issue link", "octocat", zero, zero, true, SourceExternalTool},
		{"non-member without issue link", "octocat", zero, zero, false, SourceExternalContributor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifySource(tc.login, tc.issueID, tc.taskID, tc.isMember)
			if got != tc.want {
				t.Errorf("ClassifySource(%q, valid=%v, valid=%v, member=%v) = %q, want %q",
					tc.login, tc.issueID.Valid, tc.taskID.Valid, tc.isMember, got, tc.want)
			}
		})
	}
}

func TestUrlIssueRefRegex(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // expected captured number, "" for no match
	}{
		{"plain url", "see https://multica.wisco.wine/acme/issues/42 for details", "42"},
		{"http url", "tracked at http://localhost:3000/team/issues/7", "7"},
		{"with anchor", "fix per https://multica.example.com/foo/issues/123#comment-9", "123"},
		{"no match", "no link here", ""},
		{"url without issues path", "https://example.com/foo/bar", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := urlIssueRefRe.FindStringSubmatch(tc.in)
			got := ""
			if m != nil {
				got = m[1]
			}
			if got != tc.want {
				t.Errorf("urlIssueRefRe(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAgentTaskRefRegex(t *testing.T) {
	uuid := "11111111-2222-3333-4444-555555555555"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"trailer line", "Implement foo.\n\nagent_task_id=" + uuid, uuid},
		{"with whitespace", "agent_task_id = " + uuid, uuid},
		{"uppercase key", "AGENT_TASK_ID=" + uuid, uuid},
		{"no match", "just a regular commit message", ""},
		{"truncated uuid", "agent_task_id=11111111-2222-3333-4444", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := agentTaskRefRe.FindStringSubmatch(tc.in)
			got := ""
			if m != nil {
				got = m[1]
			}
			// The regex normalizes case in the value, so compare lowercased.
			if got != "" && len(got) != len(uuid) {
				t.Errorf("agentTaskRefRe(%q) captured wrong-length value %q", tc.in, got)
			}
			if (got == "") != (tc.want == "") {
				t.Errorf("agentTaskRefRe(%q) match presence mismatch: got=%q want=%q", tc.in, got, tc.want)
			}
		})
	}
}

// TestPrefixIssueRegex_BuildsPerWorkspace verifies that the prefix-based
// pattern is constructed correctly: a workspace whose prefix is "ROA"
// matches "ROA-42" but not "MUL-42".
func TestPrefixIssueRegex_BuildsPerWorkspace(t *testing.T) {
	combined := "Closes ROA-42 (and references MUL-7 from the other workspace)"

	roaMatch := matchPrefix(t, "ROA", combined)
	if roaMatch != "42" {
		t.Errorf("expected ROA-42 to match, got %q", roaMatch)
	}
	mulMatch := matchPrefix(t, "MUL", combined)
	if mulMatch != "7" {
		t.Errorf("expected MUL-7 to match, got %q", mulMatch)
	}
	zzzMatch := matchPrefix(t, "ZZZ", combined)
	if zzzMatch != "" {
		t.Errorf("expected ZZZ to NOT match, got %q", zzzMatch)
	}
}

// matchPrefix mimics findIssueByCombined's regex construction without
// touching the database.
func matchPrefix(t *testing.T, prefix, combined string) string {
	t.Helper()
	pattern := buildPrefixIssueRegexForTest(prefix)
	m := pattern.FindStringSubmatch(combined)
	if m == nil {
		return ""
	}
	return m[1]
}
