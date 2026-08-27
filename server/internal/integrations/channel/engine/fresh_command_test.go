package engine

import "testing"

func TestParseControlCommandClassifiesSharedSyntax(t *testing.T) {
	tests := []struct {
		input string
		kind  ControlCommandKind
		body  string
	}{
		{input: "/clear reset this", kind: ControlCommandFreshSession, body: "reset this"},
		{input: "/new start this", kind: ControlCommandNewChat, body: "start this"},
	}
	for _, tc := range tests {
		command, ok := ParseControlCommand(tc.input)
		if !ok || command.Kind != tc.kind || command.Body != tc.body {
			t.Fatalf("ParseControlCommand(%q) = %+v, %v", tc.input, command, ok)
		}
	}
	if command, ok := ParseControlCommand("please /new later"); ok {
		t.Fatalf("mid-sentence control command matched: %+v", command)
	}
}

func TestParseFreshSessionCommand(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantMatch bool
		wantBody  string
	}{
		{name: "clear with same-line body", body: "/clear start from scratch", wantMatch: true, wantBody: "start from scratch"},
		{name: "leading blank lines tolerated", body: "\n\n/clear re-check the deploy", wantMatch: true, wantBody: "re-check the deploy"},
		{name: "multi-line body preserved", body: "/clear title\nline one\nline two", wantMatch: true, wantBody: "title\nline one\nline two"},
		{name: "command alone produces empty body", body: "/clear", wantMatch: true, wantBody: ""},
		{name: "prefix of token rejected", body: "/clearness is not a command", wantMatch: false},
		{name: "mid-sentence command rejected", body: "please /clear this run", wantMatch: false},
		{name: "wrong case rejected", body: "/Clear help", wantMatch: false},
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

func TestParseNewChatCommand(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantMatch bool
		wantBody  string
	}{
		{name: "new with same-line body", body: "/new investigate this", wantMatch: true, wantBody: "investigate this"},
		{name: "leading blank lines tolerated", body: "\n\t\n/new\tkeep layout\nsecond line", wantMatch: true, wantBody: "keep layout\nsecond line"},
		{name: "bare new", body: "/new", wantMatch: true, wantBody: ""},
		{name: "new wins without reparsing body", body: "/new /issue ordinary text", wantMatch: true, wantBody: "/issue ordinary text"},
		{name: "prefix rejected", body: "/newness", wantMatch: false},
		{name: "wrong case rejected", body: "/New", wantMatch: false},
		{name: "mid-sentence rejected", body: "please /new", wantMatch: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, ok := ParseNewChatCommand(tc.body)
			if ok != tc.wantMatch {
				t.Fatalf("match=%v want %v (body=%q)", ok, tc.wantMatch, body)
			}
			if body != tc.wantBody {
				t.Errorf("body=%q want %q", body, tc.wantBody)
			}
		})
	}
}
