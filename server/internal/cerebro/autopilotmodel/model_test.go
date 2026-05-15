package autopilotmodel

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty is allowed (clears override)", "", false},
		{"haiku", ModelHaiku, false},
		{"sonnet", ModelSonnet, false},
		{"opus", ModelOpus, false},
		{"unknown model", "gpt-4", true},
		{"close-but-not-quite", "claude-sonnet-4", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate(%q) err=%v wantErr=%v", tc.input, err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrUnknownModel) {
				t.Fatalf("error must wrap ErrUnknownModel, got %v", err)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.input) {
				t.Fatalf("error must include offending value, got %q", err.Error())
			}
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	textValid := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
	textNull := pgtype.Text{}

	cases := []struct {
		name         string
		taskOverride pgtype.Text
		agentModel   pgtype.Text
		want         string
	}{
		{"task override wins over agent default", textValid(ModelHaiku), textValid(ModelSonnet), ModelHaiku},
		{"falls back to agent when no override", textNull, textValid(ModelSonnet), ModelSonnet},
		{"empty override is ignored", textValid(""), textValid(ModelSonnet), ModelSonnet},
		{"both null returns empty (CLI default)", textNull, textNull, ""},
		{"both empty returns empty", textValid(""), textValid(""), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Resolve(tc.taskOverride, tc.agentModel)
			if got != tc.want {
				t.Fatalf("Resolve(%+v, %+v) = %q want %q", tc.taskOverride, tc.agentModel, got, tc.want)
			}
		})
	}
}

func TestRuntimeBriefLine(t *testing.T) {
	t.Parallel()
	if got := RuntimeBriefLine(""); got != "" {
		t.Fatalf("empty model should yield empty line, got %q", got)
	}
	got := RuntimeBriefLine(ModelHaiku)
	if !strings.Contains(got, ModelHaiku) {
		t.Fatalf("brief line must include the model id, got %q", got)
	}
}

func TestAllowedIsCheapestFirst(t *testing.T) {
	t.Parallel()
	// Order is a UI contract — Haiku first so the cost-saving option is the
	// most visible default. Regression-guard against accidental re-ordering.
	got := Allowed()
	want := []string{ModelHaiku, ModelSonnet, ModelOpus}
	if len(got) != len(want) {
		t.Fatalf("Allowed() len=%d want %d", len(got), len(want))
	}
	for i, m := range want {
		if got[i] != m {
			t.Fatalf("Allowed()[%d] = %q want %q", i, got[i], m)
		}
	}
}
