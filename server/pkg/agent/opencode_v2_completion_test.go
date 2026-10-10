package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// Fixtures follow v2.0.18 noninteractive.ts and the projected message schema.
const v2OpenStep = `{"type":"step_start","sessionID":"ses_test","part":{"messageID":"msg_reply","type":"step-start"}}` + "\n"
const v2Text = `{"type":"text","sessionID":"ses_test","part":{"messageID":"msg_reply","type":"text","text":"ok","time":{"start":100,"end":123}}}` + "\n"
const v2Tool = `{"type":"tool_use","sessionID":"ses_test","part":{"id":"tool_1","messageID":"msg_reply","type":"tool","state":{"status":"completed"}}}` + "\n"
const v2Finish = `{"type":"step_finish","sessionID":"ses_test","part":{"messageID":"msg_reply","type":"step-finish","reason":"stop"}}` + "\n"
const v2Projection = `{"data":[{"id":"msg_idle","type":"idle","outcome":"succeeded"},{"id":"msg_reply","type":"assistant","finish":"stop","time":{"completed":123},"content":[{"type":"text","text":"ok"}]}]}`

// This regression failed with "step still open at EOF" before the fix.
func TestOpencodeV2MissingStepFinish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a POSIX shell")
	}
	t.Parallel()
	for _, tc := range []struct {
		name, stream, projection, version, exit, apiExit, apiDelay string
		custom, standalone, noProbe, skipStdin                     bool
		want, errorContains                                        string
		timeout                                                    time.Duration
	}{
		{name: "text", stream: v2OpenStep + v2Text, want: "completed"},
		{name: "tool then final text", stream: strings.ReplaceAll(v2OpenStep+v2Tool, "msg_reply", "msg_tool") + v2OpenStep + v2Text, want: "completed"},
		{name: "terminal tool", stream: v2OpenStep + v2Tool, projection: strings.Replace(v2Projection, `{"type":"text","text":"ok"}`, `{"id":"tool_1","type":"tool","state":{"status":"completed"}}`, 1), want: "completed"},
		{name: "v1 stays strict", stream: v2OpenStep + v2Text, version: "1.18.25", noProbe: true, want: "failed"},
		{name: "unverified older v2", stream: v2OpenStep + v2Text, version: "opencode v2.0.10", noProbe: true, want: "failed"},
		{name: "unknown version", stream: v2OpenStep + v2Text, version: "nightly", noProbe: true, want: "failed"},
		{name: "future major", stream: v2OpenStep + v2Text, version: "3.0.0", noProbe: true, want: "failed"},
		{name: "custom profile", stream: v2OpenStep + v2Text, custom: true, noProbe: true, want: "failed"},
		{name: "prompt write failure", stream: v2OpenStep + v2Text, skipStdin: true, noProbe: true, want: "failed", errorContains: "prompt write failed"},
		{name: "nonzero exit", stream: v2OpenStep + v2Text, exit: "1", noProbe: true, want: "failed"},
		{name: "interrupt exit", stream: v2OpenStep + v2Text, exit: "130", noProbe: true, want: "failed"},
		{name: "v2 error", stream: v2OpenStep + v2Text + `{"type":"error","sessionID":"ses_test","error":{"type":"aborted","message":"Session interrupted: user"}}`, noProbe: true, want: "failed", errorContains: "Session interrupted: user"},
		{name: "v1 error", stream: v2OpenStep + v2Text + `{"type":"error","sessionID":"ses_test","error":{"name":"APIError","data":{"message":"provider failure"}}}`, noProbe: true, want: "failed", errorContains: "provider failure"},
		{name: "truncated json", stream: v2OpenStep + v2Text + `{"type":`, noProbe: true, want: "failed"},
		{name: "pending tool", stream: v2OpenStep + v2Text + strings.Replace(v2Tool, "completed", "running", 1), noProbe: true, want: "failed"},
		{name: "wrong session", stream: v2OpenStep + strings.Replace(v2Text, "ses_test", "ses_other", 1), noProbe: true, want: "failed"},
		{name: "session changes at new step", stream: v2OpenStep + v2Text + strings.ReplaceAll(v2OpenStep+v2Text, "ses_test", "ses_other"), noProbe: true, want: "failed"},
		{name: "text without step", stream: v2Text, noProbe: true, want: "failed"},
		{name: "step only", stream: v2OpenStep, want: "failed"},
		{name: "empty", noProbe: true, want: "failed"},
		{name: "partial text", stream: v2OpenStep + strings.Replace(v2Text, `"text":"ok"`, `"text":"o"`, 1), want: "failed"},
		{name: "api failure", stream: v2OpenStep + v2Text, apiExit: "1", want: "failed"},
		{name: "api invalid json", stream: v2OpenStep + v2Text, projection: `{`, want: "failed"},
		{name: "standalone cannot query exited server", stream: v2OpenStep + v2Text, standalone: true, noProbe: true, want: "failed"},
		{name: "normal terminal no probe", stream: v2OpenStep + v2Text + v2Finish, noProbe: true, want: "completed"},
		{name: "continuation gap", stream: v2OpenStep + v2Text + strings.Replace(v2Finish, `"stop"`, `"tool-calls"`, 1), noProbe: true, want: "failed"},
		{name: "empty terminal step", stream: v2OpenStep + v2Finish, noProbe: true, want: "failed"},
		{name: "timeout during verification", stream: v2OpenStep + v2Text, apiDelay: "sleep 2", timeout: 500 * time.Millisecond, want: "timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "opencode")
			projection := tc.projection
			if projection == "" {
				projection = v2Projection
			}
			version := tc.version
			if version == "" {
				version = "opencode v2.0.18"
			}
			exit := tc.exit
			if exit == "" {
				exit = "0"
			}
			apiExit := tc.apiExit
			if apiExit == "" {
				apiExit = "0"
			}
			for name, content := range map[string]string{"stream": tc.stream, "projection": projection} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			stdin := "cat >/dev/null"
			if tc.skipStdin {
				stdin = ":"
			}
			writeTestExecutable(t, path, []byte(`#!/bin/sh
if [ "$1" = api ]; then
 printf '%s\n' "$@" > "$FIXTURE_DIR/api-args"
 printf '%s\n' "$PWD" > "$FIXTURE_DIR/api-pwd"
 `+tc.apiDelay+`
 cat "$FIXTURE_DIR/projection"
 exit `+apiExit+`
fi
`+stdin+`
cat "$FIXTURE_DIR/stream"
exit `+exit+`
`))
			b := &opencodeBackend{cfg: Config{ExecutablePath: path, CLIVersion: version, BuiltinRuntime: !tc.custom, Logger: slog.Default(), Env: map[string]string{"FIXTURE_DIR": dir}}}
			timeout := tc.timeout
			if timeout == 0 {
				timeout = 10 * time.Second
			}
			args := []string{"--server", "http://fixture.invalid:4567"}
			if tc.standalone {
				args = append(args, "--standalone")
			}
			prompt := "reply ok"
			if tc.skipStdin {
				prompt = strings.Repeat("x", 1024*1024)
			}
			session, err := b.Execute(context.Background(), prompt, ExecOptions{Cwd: dir, Timeout: timeout, CustomArgs: args})
			if err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			result := <-session.Result
			if result.Status != tc.want {
				t.Fatalf("result=%+v, want %s", result, tc.want)
			}
			if tc.errorContains != "" && !strings.Contains(result.Error, tc.errorContains) {
				t.Fatalf("error=%q", result.Error)
			}
			if tc.want == "completed" && (result.Error != "" || result.Usage != nil) {
				t.Fatalf("result=%+v", result)
			}
			if tc.name == "text" && result.Output != "ok" {
				t.Fatalf("output=%q", result.Output)
			}
			apiArgs, err := os.ReadFile(filepath.Join(dir, "api-args"))
			if tc.noProbe {
				if !os.IsNotExist(err) {
					t.Fatalf("unexpected verification: %s (%v)", apiArgs, err)
				}
				return
			}
			if tc.want == "timeout" {
				return
			} // Deadline may fire before launching the probe on slow hosts.
			if err != nil {
				t.Fatal(err)
			}
			if string(apiArgs) != "api\nGET\n/api/session/ses_test/message?order=desc&limit=2\n--server\nhttp://fixture.invalid:4567\n" {
				t.Fatalf("api args=%q", apiArgs)
			}
			pwd, err := os.ReadFile(filepath.Join(dir, "api-pwd"))
			if err != nil || strings.TrimSpace(string(pwd)) != dir {
				t.Fatalf("api cwd=%q, err=%v", pwd, err)
			}
		})
	}
}

func TestOpencodeV2CompletionProjection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, old, new string
		want           bool
	}{
		{name: "complete", want: true},
		{name: "failed idle", old: "succeeded", new: "failed"},
		{name: "interrupted idle", old: "succeeded", new: "interrupted"},
		{name: "no idle", old: `"type":"idle"`, new: `"type":"user"`},
		{name: "different turn", old: "msg_reply", new: "msg_previous"},
		{name: "incomplete message", old: `"completed":123`, new: `"created":123`},
		{name: "tool continuation", old: `"finish":"stop"`, new: `"finish":"tool-calls"`},
		{name: "truncated provider", old: `"finish":"stop"`, new: `"finish":"length"`},
		{name: "message error", old: `"finish":"stop"`, new: `"finish":"stop","error":{"message":"failed"}`},
		{name: "unseen text", old: `"text":"ok"`, new: `"text":"ok more"`},
		{name: "unseen tool", old: `"text":"ok"`, new: `"text":"ok"},{"type":"tool","id":"tool_1","state":{"status":"completed"}`},
		{name: "pending tool", old: `"text":"ok"`, new: `"text":"ok"},{"type":"tool","id":"tool_1","state":{"status":"running"}`},
		{name: "unknown content", old: `"type":"text"`, new: `"type":"unknown"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &opencodeV2Completion{messageID: "msg_reply"}
			c.text.WriteString("ok")
			data := v2Projection
			if tc.old != "" {
				data = strings.Replace(data, tc.old, tc.new, 1)
			}
			if got := c.matches([]byte(data)); got != tc.want {
				t.Fatalf("matches=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestOpencodeV2ReadErrorCannotRecover(t *testing.T) {
	b := &opencodeBackend{cfg: Config{CLIVersion: "opencode v2.0.18", BuiltinRuntime: true, Logger: slog.Default()}}
	r := io.MultiReader(strings.NewReader(v2OpenStep+v2Text), iotest.ErrReader(errors.New("broken stream")))
	got := b.processEvents(r, make(chan Message, 10))
	if got.status != "failed" || got.v2Completion != nil || !strings.Contains(got.errMsg, "broken stream") {
		t.Fatalf("result=%+v", got)
	}
}
