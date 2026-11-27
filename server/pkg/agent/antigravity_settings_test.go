package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAntigravitySessionSettingsDelivery(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	for _, tc := range []struct {
		name, help              string
		helpFails               bool
		custom, extra           []string
		wantSettings, wantProbe bool
	}{
		{name: "inline JSON", extra: []string{"--settings", "/tmp/daemon-settings.json"}, help: "  --settings string  Session settings", custom: []string{"--settings", `{"orchestrator":{"planner":"Planner (High)","executor":"Executor (Low)"}}`}, wantSettings: true, wantProbe: true},
		{name: "file equals form", help: "  --settings string  Session settings", custom: []string{"--settings=/tmp/agent settings.json"}, wantSettings: true, wantProbe: true},
		{name: "old CLI", help: "Usage: agy", custom: []string{"--settings", "{}"}, wantProbe: true},
		{name: "different flag", help: "  --settings-file string", custom: []string{"--settings", "{}"}, wantProbe: true},
		{name: "failed help", help: "  --settings string", helpFails: true, custom: []string{"--settings", "{}"}, wantProbe: true},
		{name: "no settings", help: "  --settings string", custom: []string{"--add-dir", "/extra"}},
		{name: "daemon extra args remain blocked", help: "  --settings string", extra: []string{"--settings", "/tmp/daemon-settings.json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			argvPath := filepath.Join(dir, "argv")
			probePath := filepath.Join(dir, "help-called")
			executable := filepath.Join(dir, "fake-agy")
			exit := "0"
			if tc.helpFails {
				exit = "2"
			}
			script := "#!/bin/sh\nif [ \"$1\" = '--help' ]; then\n printf called > \"$PROBE_PATH\"\n printf '%s\\n' \"$HELP_TEXT\"\n exit " + exit + "\nfi\nprintf '%s\\0' \"$@\" > \"$ARGV_PATH\"\nprintf 'Done\\n'\n"
			writeTestExecutable(t, executable, []byte(script))
			backend, err := New("antigravity", Config{ExecutablePath: executable, Logger: quietAntigravityLogger(), Env: map[string]string{"HOME": dir, "HELP_TEXT": tc.help, "ARGV_PATH": argvPath, "PROBE_PATH": probePath}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "preserve this prompt", ExecOptions{Cwd: dir, CustomArgs: append(slices.Clone(tc.custom), "--output-format", "text", "--conversation", "wrong-session"), ExtraArgs: tc.extra})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			select {
			case result := <-session.Result:
				if result.Status != "completed" {
					t.Fatalf("result=%+v", result)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			data, err := os.ReadFile(argvPath)
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
			hasSettings := slices.ContainsFunc(args, func(s string) bool { return s == "--settings" || strings.HasPrefix(s, "--settings=") })
			if hasSettings != tc.wantSettings {
				t.Errorf("settings present=%v want=%v; argv=%q", hasSettings, tc.wantSettings, args)
			}
			if tc.wantSettings {
				for _, arg := range tc.custom {
					if !slices.Contains(args, arg) {
						t.Errorf("custom argument changed: %q; argv=%q", arg, args)
					}
				}
			}
			if slices.Contains(args, "text") || slices.Contains(args, "wrong-session") {
				t.Errorf("protocol override leaked: %q", args)
			}
			if slices.Contains(args, "/tmp/daemon-settings.json") {
				t.Errorf("daemon settings leaked: %q", args)
			}
			_, err = os.Stat(probePath)
			if (err == nil) != tc.wantProbe {
				t.Errorf("help probe=%v want=%v", err == nil, tc.wantProbe)
			}
		})
	}
}
