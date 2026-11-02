package agent

import (
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestOpencodeUsesACP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfg  Config
		opts ExecOptions
		want bool
	}{
		{"1.x current", Config{BuiltinRuntime: true, CLIVersion: "1.19.4"}, ExecOptions{}, true},
		{"1.x first with effort option", Config{BuiltinRuntime: true, CLIVersion: "1.15.0"}, ExecOptions{}, true},
		{"1.x too old", Config{BuiltinRuntime: true, CLIVersion: "1.14.9"}, ExecOptions{}, false},
		{"2.x as reported", Config{BuiltinRuntime: true, CLIVersion: "opencode v2.2.0"}, ExecOptions{}, true},
		{"2.0.0", Config{BuiltinRuntime: true, CLIVersion: "opencode v2.0.0"}, ExecOptions{}, true},
		{"unknown version", Config{BuiltinRuntime: true}, ExecOptions{}, false},
		{"custom profile", Config{BuiltinRuntime: false, CLIVersion: "1.19.4"}, ExecOptions{}, false},
		{"launch prefix", Config{BuiltinRuntime: true, CLIVersion: "1.19.4", LaunchPrefix: []string{"--x"}}, ExecOptions{}, false},
		{"custom_args are run flags", Config{BuiltinRuntime: true, CLIVersion: "1.19.4"}, ExecOptions{CustomArgs: []string{"--agent", "plan"}}, false},
		{"forced run", Config{BuiltinRuntime: true, CLIVersion: "1.19.4", Env: map[string]string{opencodeTransportEnv: "RUN"}}, ExecOptions{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, reason := opencodeUsesACP(tc.cfg, tc.opts); got != tc.want {
				t.Fatalf("opencodeUsesACP = %v (%s), want %v", got, reason, tc.want)
			}
		})
	}
}

func TestOpencodeACPSplitModel(t *testing.T) {
	t.Parallel()
	cases := []struct{ model, level, wantModel, wantLevel string }{
		{"openai/gpt-5", "high", "openai/gpt-5", "high"},
		{"openai/gpt-5#max", "high", "openai/gpt-5", "max"},
		{"openai/gpt-5#", "high", "openai/gpt-5", "high"},
		{"", "high", "", "high"},
	}
	for _, tc := range cases {
		m, l := opencodeACPSplitModel(tc.model, tc.level)
		if m != tc.wantModel || l != tc.wantLevel {
			t.Fatalf("split(%q,%q) = %q,%q want %q,%q", tc.model, tc.level, m, l, tc.wantModel, tc.wantLevel)
		}
	}
}

func TestOpencodeACPMcpServers(t *testing.T) {
	t.Parallel()
	cfg := Config{Logger: slog.Default()}
	raw := json.RawMessage(`{"mcpServers":{
		"local":{"command":"node","args":["s.js"],"env":{"B":"2","A":"1"}},
		"remote":{"url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer x"}},
		"off":{"command":"x","enabled":false}
	}}`)
	got, err := opencodeACPMcpServers(raw, cfg)
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	data, _ := json.Marshal(got)
	want := `[{"args":["s.js"],"command":"node","env":[{"name":"A","value":"1"},{"name":"B","value":"2"}],"name":"local"},` +
		`{"headers":[{"name":"Authorization","value":"Bearer x"}],"name":"remote","type":"http","url":"https://example.invalid/mcp"}]`
	if string(data) != want {
		t.Fatalf("servers =\n%s\nwant\n%s", data, want)
	}

	if _, err := opencodeACPMcpServers(json.RawMessage(`{"mcp":{"r":{"type":"remote","url":"https://x","oauth":{"clientId":"c"}}}}`), cfg); err == nil || !strings.Contains(err.Error(), "OAuth") {
		t.Fatalf("OAuth server must be refused, got %v", err)
	}
	if _, err := opencodeACPMcpServers(json.RawMessage(`{"mcp":{"inherited":{"enabled":false}}}`), cfg); err == nil {
		t.Fatal("bare enabled override must be refused")
	}
	if got, err := opencodeACPMcpServers(nil, cfg); err != nil || len(got) != 0 {
		t.Fatalf("empty config = %v, %v", got, err)
	}
}

func TestOpencodeExportTurnUsage(t *testing.T) {
	t.Parallel()
	v1 := `{"info":{"id":"ses"},"messages":[
		{"info":{"role":"user","time":{"created":200}}},
		{"info":{"role":"assistant","time":{"created":100},"tokens":{"input":1000,"output":1000,"reasoning":0,"cache":{"read":0,"write":0}}}},
		{"info":{"role":"assistant","time":{"created":201},"tokens":{"input":10,"output":5,"reasoning":3,"cache":{"read":7,"write":1}}}},
		{"info":{"role":"assistant","time":{"created":300},"tokens":{"input":20,"output":6,"reasoning":4,"cache":{"read":8,"write":0}}}}]}`
	got, err := opencodeExportTurnUsage([]byte(v1), 200, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := (TokenUsage{InputTokens: 30, OutputTokens: 18, CacheReadTokens: 15, CacheWriteTokens: 1}); got != want {
		t.Fatalf("1.x usage = %+v want %+v", got, want)
	}

	// 2.x flattens message metadata and may print a banner before the JSON.
	v2 := "warning: banner\n" + `{"info":{"id":"ses"},"messages":[
		{"type":"model-switched","time":{"created":250}},
		{"type":"assistant","time":{"created":250},"tokens":{"input":9182,"output":55,"reasoning":256,"cache":{"read":3,"write":0}}},
		{"type":"assistant","time":{"created":260},"tokens":{"input":115,"output":8,"reasoning":17,"cache":{"read":9187,"write":0}}}]}`
	got, err = opencodeExportTurnUsage([]byte(v2), 200, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := (TokenUsage{InputTokens: 9297, OutputTokens: 63, CacheReadTokens: 9190}); got != want {
		t.Fatalf("2.x usage = %+v want %+v", got, want)
	}

	if _, err := opencodeExportTurnUsage([]byte("not json"), 0, false); err == nil {
		t.Fatal("expected a parse error")
	}
}

// The run transport's 2.x tool_use part carries the call id as `id` (with the
// part id moved to `partID`); 1.x uses `callID`. Both must reach CallID.
func TestOpencodeToolUseCallIDBothMajors(t *testing.T) {
	t.Parallel()
	lines := map[string]string{
		"1.x": `{"type":"tool_use","sessionID":"s","part":{"id":"prt_1","type":"tool","tool":"task","callID":"call-v1","state":{"status":"completed","input":{},"output":"ok"}}}`,
		"2.x": `{"type":"tool_use","sessionID":"s","part":{"partID":"prt_2","type":"tool","id":"call-v2","tool":"subagent","state":{"status":"completed","input":{},"output":"ok"}}}`,
	}
	want := map[string]string{"1.x": "call-v1", "2.x": "call-v2"}
	for name, line := range lines {
		b := &opencodeBackend{cfg: Config{Logger: slog.Default()}}
		ch := make(chan Message, 8)
		b.processEvents(strings.NewReader(line+"\n"), ch)
		close(ch)
		var got []Message
		for m := range ch {
			got = append(got, m)
		}
		if len(got) != 2 || got[0].Type != MessageToolUse || got[1].Type != MessageToolResult {
			t.Fatalf("%s: messages = %+v", name, got)
		}
		if got[0].CallID != want[name] || got[1].CallID != want[name] {
			t.Fatalf("%s: call ids = %q/%q, want %q", name, got[0].CallID, got[1].CallID, want[name])
		}
	}
}

// A non-tool part's `id` is a part id, never a call id.
func TestOpencodeToolCallIDIgnoresNonToolParts(t *testing.T) {
	t.Parallel()
	if got := (opencodeEventPart{ID: "prt_x", Type: "text"}).toolCallID(); got != "" {
		t.Fatalf("toolCallID = %q, want empty", got)
	}
}

func TestOpencodeACPChildUpdate(t *testing.T) {
	t.Parallel()
	if !opencodeACPChildUpdate(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","_meta":{"opencode/child-session":{"id":"ses_c"}}}`)) {
		t.Fatal("child update not recognised")
	}
	if opencodeACPChildUpdate(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","_meta":{}}`)) {
		t.Fatal("parent update recognised as child")
	}
}
