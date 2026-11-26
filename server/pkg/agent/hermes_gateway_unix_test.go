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
			writeCommittedHermesInstalls(t, home)
			path, record := filepath.Join(home, "hermes"), filepath.Join(home, "invocation")
			writeTestExecutable(t, path, []byte("#!/bin/sh\nprintf '%s\n' \"$@\" \"$HERMES_HOME\" \"$PYTHONIOENCODING\" \"$HERMES_GATEWAY_DETACHED\" > \"$RECORD\"\nif [ \"$MODE\" = exit ]; then exit 7; fi\nexec sleep 60\n"))
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

func TestStartHermesTaskGatewayRepairsColdHome(t *testing.T) {
	home, tools := t.TempDir(), t.TempDir()
	path, record := filepath.Join(tools, "hermes"), filepath.Join(tools, "invocation")
	script := []string{
		"#!/bin/sh",
		"printf '%s\n' \"$@\" >> \"$RECORD\"",
		"if printf ' %s ' \"$*\" | grep -q \" pm repair \"; then",
		"\tprintf 'repair HERMES_DISABLE_LAZY_INSTALLS=%s\\n' \"${HERMES_DISABLE_LAZY_INSTALLS-}\" >> \"$RECORD\"",
		"\tif [ -n \"${HERMES_DISABLE_LAZY_INSTALLS-}\" ]; then",
		"		printf '%s\n' \"hermes: no dependency environment is committed for this install; run hermes pm repair\" >&2",
		"		exit 1",
		"	fi",
		"	mkdir -p \"$HERMES_HOME/installs/id/pm-runtime\"",
		"	printf '%s\n' '{\"generation\":\"repaired\"}' > \"$HERMES_HOME/installs/id/pm-runtime/selected.json\"",
		"	exit 0",
		"fi",
		"printf 'gateway HERMES_DISABLE_LAZY_INSTALLS=%s\\n' \"${HERMES_DISABLE_LAZY_INSTALLS-}\" >> \"$RECORD\"",
		"exec sleep 60",
	}
	writeTestExecutable(t, path, []byte(strings.Join(script, "\n")+"\n"))
	cfg := Config{ExecutablePath: path, LaunchPrefix: []string{"wrapped"}, Logger: slog.Default(),
		Env: map[string]string{"HERMES_HOME": home, "RECORD": record}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stop, err := startHermesTaskGateway(ctx, cfg, home, func(string) (bool, error) {
		if _, err := os.Stat(filepath.Join(home, "installs", "id", "pm-runtime", "selected.json")); err != nil {
			return false, nil
		}
		data, err := os.ReadFile(record)
		if err != nil || !strings.Contains(string(data), "gateway HERMES_DISABLE_LAZY_INSTALLS=") {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	stop()
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "wrapped\n--profile\ndefault\npm\nrepair\n") {
		t.Fatalf("missing pm repair invocation: %q", text)
	}
	if !strings.Contains(text, "repair HERMES_DISABLE_LAZY_INSTALLS=\n") {
		t.Fatalf("pm repair ran with lazy installs disabled: %q", text)
	}
	if !strings.Contains(text, "wrapped\n--profile\ndefault\ngateway\nrun\n--quiet\n--external-supervisor\n") {
		t.Fatalf("missing gateway invocation: %q", text)
	}
	if !strings.Contains(text, "gateway HERMES_DISABLE_LAZY_INSTALLS=1\n") {
		t.Fatalf("gateway did not disable lazy installs: %q", text)
	}
	if !hermesDependencyEnvironmentCommitted(home) {
		t.Fatal("repair did not leave a committed dependency environment")
	}
}

func TestStartHermesTaskGatewayRepairFailure(t *testing.T) {
	home, tools := t.TempDir(), t.TempDir()
	path, record := filepath.Join(tools, "hermes"), filepath.Join(tools, "invocation")
	script := []string{
		"#!/bin/sh",
		"printf '%s\n' \"$@\" >> \"$RECORD\"",
		"if printf ' %s ' \"$*\" | grep -q \" pm repair \"; then",
		"	exit 9",
		"fi",
		"printf 'gateway-started\n' >> \"$RECORD\"",
		"exec sleep 60",
	}
	writeTestExecutable(t, path, []byte(strings.Join(script, "\n")+"\n"))
	cfg := Config{ExecutablePath: path, Logger: slog.Default(),
		Env: map[string]string{"HERMES_HOME": home, "RECORD": record}}
	stop, err := startHermesTaskGateway(context.Background(), cfg, home, func(string) (bool, error) {
		return true, nil
	})
	if err == nil {
		stop()
		t.Fatal("expected repair failure")
	}
	if !strings.Contains(err.Error(), "hermes pm repair") {
		t.Fatalf("error = %v, want hermes pm repair", err)
	}
	got, readErr := os.ReadFile(record)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if text := string(got); strings.Contains(text, "gateway-started") || strings.Contains(text, "gateway\nrun") {
		t.Fatalf("gateway started after repair failed: %q", text)
	}
}

func TestStopHermesTaskGateway(t *testing.T) {
	home, tools := t.TempDir(), t.TempDir()
	record := filepath.Join(tools, "invocation")
	path := filepath.Join(tools, "hermes")
	writeTestExecutable(t, path, []byte("#!/bin/sh\nprintf '%s\n' \"$@\" \"$HERMES_HOME\" > \"$RECORD\"\n"))
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
			writeTestExecutable(t, path, []byte("#!/bin/sh\nprintf '%s\n' \"$@\" >> \"$RECORD\"\ncase \"$*\" in *--yes) printf '%s\n' \"$STOP_ERROR\" >&2; exit \"$STOP_EXIT\";; esac\n"))
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
