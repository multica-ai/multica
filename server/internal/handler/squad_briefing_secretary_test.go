package handler

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

func TestSecretaryIssueKindOf(t *testing.T) {
	cases := []struct {
		name              string
		originType        string
		parentBySecretary bool
		want              secretaryIssueKind
	}{
		{"directive", "directive", false, secretaryIssueDirective},
		{"directive wins over parent", "directive", true, secretaryIssueDirective},
		{"request from secretary", "squad_request", true, secretaryIssueRequest},
		{"request from another squad", "squad_request", false, secretaryIssueNone},
		{"ordinary issue", "", false, secretaryIssueNone},
		{"agent create", "agent_create", true, secretaryIssueNone},
		{"proposal", "proposal", false, secretaryIssueNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := secretaryIssueKindOf(tc.originType, tc.parentBySecretary); got != tc.want {
				t.Errorf("secretaryIssueKindOf(%q, %v) = %v, want %v", tc.originType, tc.parentBySecretary, got, tc.want)
			}
		})
	}
}

func TestSecretaryDirectiveBlockAbsentOnOtherIssues(t *testing.T) {
	if out := secretaryDirectiveBlock(secretaryIssueNone, map[string]bool{"squad-reporting": true}); out != "" {
		t.Errorf("expected no block on an ordinary issue, got:\n%s", out)
	}
}

func TestSecretaryDirectiveBlockContent(t *testing.T) {
	for _, kind := range []secretaryIssueKind{secretaryIssueDirective, secretaryIssueRequest} {
		out := secretaryDirectiveBlock(kind, map[string]bool{})
		for _, want := range []string{
			"## Reporting for the Secretary",
			"not for the boss",
			"does not apply here",
			"ask the Secretary",
			"Do not ask the boss directly",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("kind %v: block missing %q:\n%s", kind, want, out)
			}
		}
	}
}

func TestSecretaryDirectiveBlockSkillNamedOnlyWhenInstalled(t *testing.T) {
	none := secretaryDirectiveBlock(secretaryIssueDirective, map[string]bool{})
	if strings.Contains(none, "squad-reporting") || strings.Contains(none, "directive-execution") {
		t.Errorf("block names a skill that is not installed:\n%s", none)
	}

	both := secretaryDirectiveBlock(secretaryIssueDirective, map[string]bool{
		"squad-reporting":     true,
		"directive-execution": true,
	})
	if !strings.Contains(both, "`squad-reporting` skill") || !strings.Contains(both, "`directive-execution` skill") {
		t.Errorf("installed skills should be named for a directive:\n%s", both)
	}

	// directive-execution is written for directives only; a request names squad-reporting alone.
	request := secretaryDirectiveBlock(secretaryIssueRequest, map[string]bool{
		"squad-reporting":     true,
		"directive-execution": true,
	})
	if strings.Contains(request, "directive-execution") {
		t.Errorf("a request should not point to directive-execution:\n%s", request)
	}
	if !strings.Contains(request, "`squad-reporting` skill") {
		t.Errorf("a request should point to an installed squad-reporting:\n%s", request)
	}
}

func TestSecretaryDirectiveBlockDoesNotTriggerMentions(t *testing.T) {
	out := secretaryDirectiveBlock(secretaryIssueDirective, map[string]bool{"squad-reporting": true})
	if triggered := util.ParseMentions(out); len(triggered) != 0 {
		t.Errorf("leader note must not trigger ParseMentions, got %#v", triggered)
	}
}
