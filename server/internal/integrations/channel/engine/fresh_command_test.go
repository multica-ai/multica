package engine

import "testing"

func TestParseFreshSessionCommand(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantMatch bool
		wantBody  string
	}{
		{name: "new with same-line body", body: "/new start from scratch", wantMatch: true, wantBody: "start from scratch"},
		{name: "leading blank lines tolerated", body: "\n\n/new re-check the deploy", wantMatch: true, wantBody: "re-check the deploy"},
		{name: "multi-line body preserved", body: "/new title\nline one\nline two", wantMatch: true, wantBody: "title\nline one\nline two"},
		{name: "command alone produces empty body", body: "/new", wantMatch: true, wantBody: ""},
		{name: "prefix of token rejected", body: "/newness is not a command", wantMatch: false},
		{name: "mid-sentence command rejected", body: "please /new this run", wantMatch: false},
		{name: "wrong case rejected", body: "/New help", wantMatch: false},
		{name: "normal body rejected", body: "help me normally", wantMatch: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, ok := ParseFreshSessionCommand(tc.body)
			if ok != tc.wantMatch {
				t.Fatalf("match=%v want %v (body=%q)", ok, tc.wantMatch, body)
			}
			if body != tc.wantBody {
				t.Errorf("body=%q want %q", body, tc.wantBody)
			}
		})
	}
}
