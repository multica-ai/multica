//go:build !windows

package agent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartHermesTaskGatewayLifecycle(t *testing.T) {
	for _, mode := range []string{"ready", "cancel startup", "cancel running", "exit", "probe error"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			path, record := filepath.Join(home, "hermes"), filepath.Join(home, "invocation")
			writeTestExecutable(t, path, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" \"$HERMES_HOME\" \"$PYTHONIOENCODING\" \"$HERMES_GATEWAY_DETACHED\" > \"$RECORD\"\nif [ \"$MODE\" = exit ]; then exit 7; fi\nexec sleep 60\n"))
			cfg := Config{ExecutablePath: path, LaunchPrefix: []string{"wrapped"}, Logger: slog.Default(),
				Env: map[string]string{"HERMES_HOME": home, "RECORD": record, "MODE": mode, "PYTHONIOENCODING": "ascii", "HERMES_GATEWAY_DETACHED": "0"}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stop, err := startHermesTaskGateway(ctx, cfg, home, func(string) (bool, error) {
				if _, err := os.Stat(record); err != nil {
					return false, nil
				}
				switch mode {
				case "cancel startup":
					cancel()
					return false, nil
				case "probe error":
					return false, errors.New("probe failed")
				case "exit":
					return false, nil
				}
				return true, nil
			})
			if mode == "ready" || mode == "cancel running" {
				if err != nil {
					t.Fatal(err)
				}
				if mode == "cancel running" {
					cancel()
				}
				stop()
				stop() // Deferred cleanup may follow cancellation.
			} else if err == nil {
				stop()
				t.Fatal("expected startup failure")
			}
			got, readErr := os.ReadFile(record)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := "wrapped\n--profile\ndefault\ngateway\nrun\n--quiet\n--external-supervisor\n" + home + "\nutf-8\n1\n"
			if string(got) != want {
				t.Fatalf("invocation = %q, want %q", got, want)
			}
		})
	}
}

func TestStopHermesTaskGateway(t *testing.T) {
	home, tools := t.TempDir(), t.TempDir()
	record := filepath.Join(tools, "invocation")
	path := filepath.Join(tools, "hermes")
	writeTestExecutable(t, path, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" \"$HERMES_HOME\" > \"$RECORD\"\n"))
	cfg := Config{ExecutablePath: path, LaunchPrefix: []string{"wrapped"}, Logger: slog.Default(),
		Env: map[string]string{"HERMES_HOME": home, "RECORD": record}}
	if err := StopHermesTaskGateway(cfg, home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("in-process ACP must not invoke gateway cleanup")
	}
	if err := os.WriteFile(filepath.Join(home, "gateway.lock"), []byte("owned gateway"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := StopHermesTaskGateway(cfg, home); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if want := "wrapped\n--profile\ndefault\ngateway\nstop\n--yes\n" + home + "\n"; string(got) != want {
		t.Fatalf("cleanup invocation = %q, want %q", got, want)
	}
}

func TestStopHermesTaskGatewayRejectsHostState(t *testing.T) {
	home, host := t.TempDir(), t.TempDir()
	path := filepath.Join(host, "gateway.pid")
	if err := os.WriteFile(path, []byte("host gateway"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, filepath.Join(home, "gateway.pid")); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ExecutablePath: filepath.Join(home, "must-not-run"), Env: map[string]string{"HERMES_HOME": home}}
	if err := StopHermesTaskGateway(cfg, home); err == nil || !strings.Contains(err.Error(), "non-local") {
		t.Fatalf("cleanup through host PID link = %v, want refusal", err)
	}
}

func TestStopHermesTaskGatewayLegacyFlag(t *testing.T) {
	for _, tc := range []struct {
		name, output, code string
		wantCalls          int
		wantErr            bool
	}{
		{"unsupported flag", "hermes: error: unrecognized arguments: --yes", "2", 2, false},
		{"busy gateway", "Refusing to stop the gateway while sessions are live", "1", 1, true},
		{"other parser error", "hermes: error: unrecognized arguments: --profile", "2", 1, true},
		{"runtime error", "hermes: error: unrecognized arguments: --yes", "1", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, tools := t.TempDir(), t.TempDir()
			record, path := filepath.Join(tools, "invocation"), filepath.Join(tools, "hermes")
			writeTestExecutable(t, path, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$RECORD\"\ncase \"$*\" in *--yes) printf '%s\\n' \"$STOP_ERROR\" >&2; exit \"$STOP_EXIT\";; esac\n"))
			if err := os.WriteFile(filepath.Join(home, "gateway.lock"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{
				"HERMES_HOME": home, "RECORD": record, "STOP_ERROR": tc.output, "STOP_EXIT": tc.code,
			}}
			if err := StopHermesTaskGateway(cfg, home); (err != nil) != tc.wantErr {
				t.Fatalf("stop error = %v, wantErr=%v", err, tc.wantErr)
			}
			got, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			want := "--profile\ndefault\ngateway\nstop\n--yes\n"
			if tc.wantCalls == 2 {
				want += "--profile\ndefault\ngateway\nstop\n"
			}
			if string(got) != want {
				t.Fatalf("cleanup invocations = %q, want %q", got, want)
			}
		})
	}
}
