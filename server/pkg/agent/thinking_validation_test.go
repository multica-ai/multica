package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCheckThinkingLevelWith(t *testing.T) {
	t.Parallel()
	listed := Model{ID: "listed", Default: true, Thinking: &ModelThinking{
		SupportedLevels: []ThinkingLevel{{Value: "medium"}, {Value: "max"}},
	}}
	live := Catalog{Models: []Model{listed, {ID: "no-effort"}}, Unavailable: []UnavailableModel{{ID: "disabled[1m]"}}}
	for _, level := range []string{"medium", "max"} {
		for _, tc := range []struct {
			name     string
			provider string
			model    string
			catalog  Catalog
			valid    bool
			unknown  bool
			reason   string
		}{
			{name: "listed", provider: "claude", model: "listed", catalog: live, valid: true},
			{name: "default", provider: "claude", catalog: live, valid: true},
			{name: "unlisted Claude", provider: "claude", model: "unlisted", catalog: live, unknown: true},
			{name: "unlisted context variant", provider: "claude", model: "unlisted[1m]", catalog: live, unknown: true},
			{name: "malformed context variant", provider: "claude", model: "listed[weird]", catalog: live, reason: "not found"},
			{name: "no effort", provider: "claude", model: "no-effort", catalog: live, reason: "does not advertise thinking-level control"},
			{name: "disabled", provider: "claude", model: "disabled", catalog: live, reason: "unavailable"},
			{name: "old CLI", provider: "claude", model: "unlisted", catalog: Catalog{Models: live.Models, CLIThinkingLevels: []string{}}, reason: "CLI does not support"},
			{name: "CLI vocabulary", provider: "claude", model: "unlisted", catalog: Catalog{Models: live.Models, CLIThinkingLevels: []string{"high"}}, reason: "CLI does not accept"},
			{name: "fallback", provider: "claude", model: "listed", catalog: Catalog{Models: live.Models, Fallback: true}, unknown: true},
			{name: "empty catalog", provider: "claude", model: "listed", unknown: true},
			{name: "missing default", provider: "claude", catalog: Catalog{Models: []Model{{ID: "other"}}}, reason: "default model could not be resolved"},
			{name: "Codex explicit", provider: "codex", model: "unlisted", catalog: live, reason: "not found"},
			{name: "OMP explicit", provider: "omp", model: "unlisted", catalog: live, reason: "not found"},
			{name: "OpenCode explicit", provider: "opencode", model: "unlisted", catalog: live, reason: "not found"},
			{name: "Codex default", provider: "codex", catalog: live, reason: "codex requires an explicit model"},
			{name: "OMP default", provider: "omp", catalog: live, reason: "omp requires an explicit model"},
		} {
			t.Run(level+"/"+tc.name, func(t *testing.T) {
				reads := 0
				check, err := CheckThinkingLevelWith(func() (Catalog, error) {
					reads++
					return tc.catalog, nil
				}, tc.provider, tc.model, level)
				if check.Valid != tc.valid || errors.Is(err, errUnverifiedCatalog) != tc.unknown || (err != nil && !tc.unknown) {
					t.Fatalf("got (%+v, %v), want valid=%v, unknown=%v", check, err, tc.valid, tc.unknown)
				}
				if !strings.Contains(check.Reason, tc.reason) || (tc.reason == "" && check.Reason != "") {
					t.Errorf("reason = %q, want %q", check.Reason, tc.reason)
				}
				wantReads := 1
				if tc.model == "" && ThinkingRequiresExplicitModel(tc.provider) {
					wantReads = 0
				}
				if reads != wantReads {
					t.Errorf("catalog reads = %d, want %d", reads, wantReads)
				}
			})
		}
	}
	t.Run("known unsupported level", func(t *testing.T) {
		check, err := CheckThinkingLevelWith(func() (Catalog, error) { return live, nil }, "claude", "listed", "high")
		if err != nil || check.Valid || !strings.Contains(check.Reason, "does not support this thinking level") {
			t.Fatalf("got (%+v, %v), want a confirmed unsupported-level rejection", check, err)
		}
	})
	t.Run("lookup failure", func(t *testing.T) {
		check, err := CheckThinkingLevelWith(func() (Catalog, error) { return Catalog{}, context.DeadlineExceeded }, "claude", "listed", "medium")
		if !errors.Is(err, context.DeadlineExceeded) || check.Valid || check.Reason != "" {
			t.Fatalf("got (%+v, %v), want lookup failure without a rejection reason", check, err)
		}
	})
}
