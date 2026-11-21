package agent

import (
	"slices"
	"strings"
	"testing"
)

// codexRetiredCompactionLiveError is the failure exactly as GH #8000 reported
// it, request id and cf-ray included — the hex ids are part of the fixture on
// purpose, since a status-code match would find digits inside them.
const codexRetiredCompactionLiveError = `Error running remote compact task: unexpected status 404 Not Found: ` +
	`{"detail":"Not Found"}, url: https://chatgpt.com/backend-api/codex/responses/compact, ` +
	`cf-ray: a35507973eee3d57-SJC, request id: 9eed88ee-822d-446e-9b73-df49d56fe7e0`

func TestCodexRetiredCompactionError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		errText string
		want    bool
	}{
		{"live failure", codexRetiredCompactionLiveError, true},
		// The path is what identifies the route, so any status on it counts:
		// a compaction request reaching /responses/compact at all means the
		// legacy route was selected, and the remedy is the same either way.
		{
			"retired path with a non-404 status",
			`Error running remote compact task: unexpected status 502 Bad Gateway, ` +
				`url: https://chatgpt.com/backend-api/codex/responses/compact`,
			true,
		},
		{
			"case insensitive",
			`ERROR RUNNING REMOTE COMPACT TASK: UNEXPECTED STATUS 404 NOT FOUND, ` +
				`URL: HTTPS://CHATGPT.COM/BACKEND-API/CODEX/RESPONSES/COMPACT`,
			true,
		},

		// The reason the path is required rather than the status. Codex wraps
		// v2 compaction failures in the SAME "Error running remote compact
		// task" prefix (compact_remote_v2.rs), and a v2 stream can answer 404
		// too. Firing here would tell a user whose setting is already correct
		// to go turn it off — the exact opposite of the fix.
		{
			"v2 route returning the same status",
			`Error running remote compact task: unexpected status 404 Not Found: {"detail":"Not Found"}, ` +
				`url: https://chatgpt.com/backend-api/codex/responses`,
			false,
		},
		{
			"v2 route failing some other way",
			`Error running remote compact task: remote compaction v2 stream closed before response.completed`,
			false,
		},
		// The accepted false negative: without the url tail nothing in the
		// text says which route ran, and guessing would land on the case
		// above. No hint costs what today already costs; a wrong hint is worse.
		{
			"no url tail",
			`Error running remote compact task: unexpected status 404 Not Found: {"detail":"Not Found"}`,
			false,
		},
		// The path without the compaction marker is some other request.
		{
			"path outside a compaction failure",
			`Error running turn: unexpected status 404 Not Found, ` +
				`url: https://chatgpt.com/backend-api/codex/responses/compact`,
			false,
		},
		{"unrelated codex failure", "codex thread/resume failed: token too long", false},
		{"empty", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CodexRetiredCompactionError(tc.errText); got != tc.want {
				t.Errorf("CodexRetiredCompactionError(%q) = %v, want %v", tc.errText, got, tc.want)
			}
		})
	}
}

// TestCodexLaunchArgsCanSelectTheRetiredCompactionRoute is the behavioural
// basis for the hint naming launch arguments alongside the config file.
//
// The remedy text is a constant, so it cannot know which source turned the
// setting off — which makes "is an argument even a real source?" a claim the
// code has to back. It is: codexBlockedArgs blocks only --listen, and every
// feature-conflict filter here is scoped to fast_mode or the managed
// mcp_servers namespace. So both spellings survive from all three argv regions
// the hint names — an agent's custom args, a daemon's extra args, and a custom
// runtime profile's fixed args — and the config file is not the only place to
// look.
//
// If a future change starts stripping these — the #8019 override, say — this
// test fails, which is the signal to drop that half of the hint rather than
// keep pointing users at a source that no longer exists.
func TestCodexLaunchArgsCanSelectTheRetiredCompactionRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
	}{
		{"disable flag", []string{"--disable", "remote_compaction_v2"}},
		{"disable flag with inline value", []string{"--disable=remote_compaction_v2"}},
		{"config override", []string{"-c", "features.remote_compaction_v2=false"}},
		{"config override shell-quoted", []string{"'-c'", "'features.remote_compaction_v2=false'"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Both argv regions, since custom_args and a daemon's extra args
			// are separate inputs and the hint names both.
			for region, args := range map[string][]string{
				"extra_args":  tc.args,
				"custom_args": tc.args,
			} {
				var extra, custom []string
				if region == "extra_args" {
					extra = args
				} else {
					custom = args
				}
				got := NormalizeCodexLaunchArgs(extra, custom, nil, nil)
				if !slices.Contains(got, "remote_compaction_v2") &&
					!slices.Contains(got, "--disable=remote_compaction_v2") &&
					!slices.Contains(got, "features.remote_compaction_v2=false") {
					t.Errorf("%s: setting did not survive normalization: %q", region, got)
				}
			}

			// The one place this package does strip a --disable is the
			// fast_mode conflict removal under an explicit service tier.
			// It is scoped to that feature, and the effective argv is where
			// that has to hold.
			effective := buildCodexArgs(ExecOptions{
				CustomArgs:  tc.args,
				ServiceTier: codexFastServiceTier,
			}, nil)
			if !mentionsRemoteCompactionV2(effective) {
				t.Errorf("priority tier stripped an unrelated setting: %q", effective)
			}

			// The third argv region the hint names: a custom runtime profile's
			// fixed args. It reaches the child through the launch prefix, not
			// through NormalizeCodexLaunchArgs, so it has its own filters —
			// all three of which the setting has to survive for the hint to be
			// telling the truth about where to look.
			prefix := FilterLaunchPrefix("codex", tc.args, nil)
			if !mentionsRemoteCompactionV2(prefix) {
				t.Errorf("launch prefix filter dropped the setting: %q", prefix)
			}
			if got := filterCodexCustomConfigOverrides(prefix, nil); !mentionsRemoteCompactionV2(got) {
				t.Errorf("managed mcp_config prefix filter dropped the setting: %q", got)
			}
			if got := stripCodexFastModeConflicts(prefix, nil); !mentionsRemoteCompactionV2(got) {
				t.Errorf("priority tier prefix filter dropped the setting: %q", got)
			}
		})
	}
}

// mentionsRemoteCompactionV2 reports whether the setting survived in any of its
// spellings — as a standalone --disable value, an inline one, or a -c override.
func mentionsRemoteCompactionV2(args []string) bool {
	return slices.ContainsFunc(args, func(arg string) bool {
		return strings.Contains(arg, "remote_compaction_v2")
	})
}
