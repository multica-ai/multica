package daemon

import (
	"encoding/json"
	"testing"
)

// workspaceCoAuthoredByEnabled gates the prepare-commit-msg hook installed in
// agent worktrees. RFC MUL-2414 adds the `github_enabled` master switch:
// when it is explicitly false the hook must NOT be installed even if
// `co_authored_by_enabled` is true. The function also defaults to true
// whenever settings are absent or malformed so existing workspaces keep
// their historical behavior.
func TestWorkspaceCoAuthoredByEnabled(t *testing.T) {
	cases := []struct {
		name       string
		register   bool
		settings   string
		want       bool
	}{
		{"unknown workspace defaults on", false, "", true},
		{"registered workspace, nil settings defaults on", true, "", true},
		{"empty object defaults on", true, "{}", true},
		{"co_authored_by absent defaults on", true, `{"github_enabled":true}`, true},
		{"co_authored_by true", true, `{"co_authored_by_enabled":true}`, true},
		{"co_authored_by false", true, `{"co_authored_by_enabled":false}`, false},
		{
			"master off forces hook off even when co_authored_by true",
			true,
			`{"github_enabled":false,"co_authored_by_enabled":true}`,
			false,
		},
		{
			"master on lets co_authored_by decide",
			true,
			`{"github_enabled":true,"co_authored_by_enabled":false}`,
			false,
		},
		{"malformed settings defaults on", true, `not json`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &Daemon{workspaces: make(map[string]*workspaceState)}
			if tc.register {
				var raw json.RawMessage
				if tc.settings != "" {
					raw = json.RawMessage(tc.settings)
				}
				d.workspaces["ws"] = newWorkspaceState("ws", nil, "", nil, raw)
			}
			if got := d.workspaceCoAuthoredByEnabled("ws"); got != tc.want {
				t.Fatalf("workspaceCoAuthoredByEnabled(%q) = %v, want %v",
					tc.settings, got, tc.want)
			}
		})
	}
}
