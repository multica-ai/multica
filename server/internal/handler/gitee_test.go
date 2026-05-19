package handler

import (
	"testing"
)

func TestVerifyGiteeToken(t *testing.T) {
	cases := []struct {
		name   string
		secret string
		token  string
		want   bool
	}{
		{"valid_match", "my-secret-123", "my-secret-123", true},
		{"mismatch", "my-secret-123", "wrong-secret", false},
		{"empty_token", "my-secret-123", "", false},
		{"empty_secret", "", "some-token", false},
		{"both_empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := verifyGiteeToken(tc.secret, tc.token, nil)
			if got != tc.want {
				t.Errorf("verifyGiteeToken(%q, %q) = %v, want %v",
					tc.secret, tc.token, got, tc.want)
			}
		})
	}
}

func TestDeriveGiteePRState(t *testing.T) {
	cases := []struct {
		state    string
		mergedAt string
		want     string
	}{
		{"open", "", "open"},
		{"closed", "", "closed"},
		{"open", "2026-05-01T00:00:00Z", "merged"},
		{"closed", "2026-05-01T00:00:00Z", "merged"},
		{"merged", "2026-05-01T00:00:00Z", "merged"},
	}
	for _, tc := range cases {
		got := deriveGiteePRState(tc.state, tc.mergedAt)
		if got != tc.want {
			t.Errorf("deriveGiteePRState(%q, %q) = %q, want %q",
				tc.state, tc.mergedAt, got, tc.want)
		}
	}
}

func TestExtractIdentifiers_GiteePatterns(t *testing.T) {
	// Verify that identifier extraction works with typical Gitee PR patterns.
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "gitee_branch_name",
			in:   []string{"", "", "feat/ope-918-gitee-pr-integration"},
			want: []string{"OPE-918"},
		},
		{
			name: "gitee_title",
			in:   []string{"feat: 支持 Gitee PR 关联 (OPE-918)", "", ""},
			want: []string{"OPE-918"},
		},
		{
			name: "gitee_body_with_issue_reference",
			in:   []string{"", "Issue: OPE-918\nAlso fixes OPE-100", ""},
			want: []string{"OPE-918", "OPE-100"},
		},
		{
			name: "gitee_all_sources",
			in:   []string{"fix(auth): login bug (OPE-42)", "Closes OPE-42", "fix/ope-42-login"},
			want: []string{"OPE-42"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractIdentifiers(tc.in...)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if len(got) != len(tc.want) {
				t.Errorf("extractIdentifiers() = %v, want %v", got, tc.want)
				return
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("extractIdentifiers()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
