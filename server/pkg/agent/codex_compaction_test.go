package agent

import "testing"

// codexRetiredCompactionLiveError is the failure exactly as GH #8000 reported
// it, request id and cf-ray included — the hex ids are part of the fixture on
// purpose, since a bare "404" substring match would find one inside them.
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
		// Older/shorter renderings of the same failure: the upstream issue
		// quotes it without the url tail, and the status phrase alone has to
		// carry it there.
		{
			"no url tail",
			`Error running remote compact task: unexpected status 404 Not Found: {"detail":"Not Found"}`,
			true,
		},
		// The path is evidence of the stale flag whatever status came back —
		// a compaction call reaching that route at all means v2 is disabled.
		{
			"retired path without a 404",
			`Error running remote compact task: unexpected status 502 Bad Gateway, ` +
				`url: https://chatgpt.com/backend-api/codex/responses/compact`,
			true,
		},
		{"case insensitive", `ERROR RUNNING REMOTE COMPACT TASK: UNEXPECTED STATUS 404 NOT FOUND`, true},

		// Compaction failures the remedy does not fix. Pointing these at the
		// config key would send users to edit a file that was never the
		// problem, so the marker alone must not be enough.
		{
			"transient status on the v2 route",
			`Error running remote compact task: unexpected status 503 Service Unavailable, ` +
				`url: https://chatgpt.com/backend-api/codex/responses`,
			false,
		},
		{
			"rate limited compaction",
			`Error running remote compact task: unexpected status 429 Too Many Requests`,
			false,
		},
		// A 404 from somewhere else in the turn says nothing about compaction.
		{
			"unrelated 404",
			`Error running turn: unexpected status 404 Not Found, url: https://chatgpt.com/backend-api/codex/responses`,
			false,
		},
		// The digits of a request id are not a status code.
		{
			"404 only inside a request id",
			`Error running remote compact task: stream disconnected, request id: 9eed404e-822d-446e-9b73-df49d56fe7e0`,
			false,
		},
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
