package engine

import "testing"

func TestParseProjectCommand(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		ok       bool
		resource string
		action   string
		args     []string
		raw      string
	}{
		{name: "project bind", body: "/project bind Backend", ok: true, resource: "project", action: "bind", args: []string{"Backend"}, raw: "Backend"},
		{name: "issue create", body: "/issue create Fix login\nMore detail", ok: true, resource: "issue", action: "create", args: []string{"Fix", "login"}, raw: "Fix login\nMore detail"},
		{name: "issue shortcut", body: "/issue MUL-123", ok: true, resource: "issue", action: "bind", args: []string{"MUL-123"}, raw: "MUL-123"},
		{name: "help", body: "/help", ok: true, resource: "help", action: "show"},
		{name: "unknown action stays deterministic", body: "/issue frobnicate", ok: true, resource: "issue", action: "bind", args: []string{"frobnicate"}, raw: "frobnicate"},
		{name: "case sensitive", body: "/Project bind Backend", ok: false},
		{name: "inline prefix", body: "please /project status", ok: false},
		{name: "prefix token", body: "/projector status", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseProjectCommand(tt.body)
			if ok != tt.ok {
				t.Fatalf("ok=%v want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if got.Resource != tt.resource || got.Action != tt.action || got.RawArguments != tt.raw {
				t.Fatalf("got %+v", got)
			}
			if len(got.Arguments) != len(tt.args) {
				t.Fatalf("args=%v want %v", got.Arguments, tt.args)
			}
			for i := range tt.args {
				if got.Arguments[i] != tt.args[i] {
					t.Fatalf("args=%v want %v", got.Arguments, tt.args)
				}
			}
		})
	}
}
