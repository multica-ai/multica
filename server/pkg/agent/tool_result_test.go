package agent

import (
	"encoding/json"
	"testing"
)

func TestNormalizeToolResultOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{
			name: "json string decodes one level to real newlines and quotes",
			raw:  json.RawMessage(`"line1\nline2 said \"hi\""`),
			want: "line1\nline2 said \"hi\"",
		},
		{
			name: "json object stays raw and is not unwrapped",
			raw:  json.RawMessage(`{"a":1}`),
			want: `{"a":1}`,
		},
		{
			name: "json array stays raw",
			raw:  json.RawMessage(`[1,2,3]`),
			want: `[1,2,3]`,
		},
		{
			name: "plain stdout with real backslash and newlines is unchanged",
			raw:  json.RawMessage("C:\\path\nline2"),
			want: "C:\\path\nline2",
		},
		{
			name: "empty input returns empty string",
			raw:  json.RawMessage(``),
			want: "",
		},
		{
			name: "nil input returns empty string",
			raw:  nil,
			want: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeToolResultOutput(tc.raw); got != tc.want {
				t.Fatalf("normalizeToolResultOutput(%q) = %q, want %q", string(tc.raw), got, tc.want)
			}
		})
	}
}
