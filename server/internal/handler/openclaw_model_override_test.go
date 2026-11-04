package handler

import "testing"

func TestOpenclawModelOverride(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "absent payload", raw: "", want: ""},
		{name: "null payload", raw: "null", want: ""},
		{name: "empty object", raw: "{}", want: ""},
		{name: "trimmed override", raw: `{"mode":"gateway","model_override":"  openrouter/@preset/agent-reasoning  "}`, want: "openrouter/@preset/agent-reasoning"},
		{name: "whitespace-only override", raw: `{"model_override":"   "}`, want: ""},
		{name: "malformed json", raw: `{"model_override":`, want: ""},
		{name: "unrelated keys are ignored", raw: `{"model_override":"m","other":1}`, want: "m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := openclawModelOverride([]byte(tc.raw)); got != tc.want {
				t.Errorf("openclawModelOverride(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
