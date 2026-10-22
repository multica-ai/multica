package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClaudeConfigDirectory(t *testing.T) {
	t.Parallel()
	cwd, home := t.TempDir(), t.TempDir()
	for _, tc := range []struct{ override, want string }{
		{"", filepath.Join(home, ".claude")},
		{"custom", filepath.Join(cwd, "custom")},
		{home, home},
	} {
		cfg := Config{Env: map[string]string{"HOME": home, "USERPROFILE": home, "CLAUDE_CONFIG_DIR": tc.override}}
		got, err := ClaudeConfigDirectory(cfg, cwd)
		if err != nil || got != tc.want {
			t.Fatalf("%q: %q, %v; want %q", tc.override, got, err, tc.want)
		}
	}
	key := "HOME"
	if runtime.GOOS == "windows" {
		key = "USERPROFILE"
	}
	if _, err := ClaudeConfigDirectory(Config{Env: map[string]string{key: "", "CLAUDE_CONFIG_DIR": ""}}, cwd); err == nil {
		t.Fatal("missing runtime home accepted")
	}
}

func TestParseClaudePluginInventory(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "null", "{}", `[null]`, `[{"id":"paper@market"}]`, `[{"id":"paper@market","enabled":null}]`, `[{"id":"paper@market","enabled":"false"}]`, `[{"enabled":false}]`} {
		if _, err := parseClaudePluginInventory([]byte(raw)); err == nil {
			t.Errorf("accepted incomplete inventory: %s", raw)
		}
	}
	got, err := parseClaudePluginInventory([]byte(`[{"id":"paper@market","scope":"user","enabled":false,"installPath":"/host/paper","future":true}]`))
	if err != nil || len(got) != 1 || got[0].Enabled || got[0].ID != "paper@market" {
		t.Fatalf("disabled plugin: %+v, %v", got, err)
	}
	if got, err := parseClaudePluginInventory([]byte(`[]`)); err != nil || len(got) != 0 {
		t.Fatalf("uninstalled inventory: %+v, %v", got, err)
	}
}

func TestClaudePluginInventoryHelper(t *testing.T) {
	if os.Getenv("MULTICA_TEST_CLAUDE_INVENTORY_HELPER") != "1" {
		return
	}
	cwd, _ := os.Getwd()
	record, _ := json.Marshal(map[string]any{
		"cwd": cwd, "config": os.Getenv("CLAUDE_CONFIG_DIR"), "args": os.Args[1:],
	})
	if err := os.WriteFile(os.Getenv("MULTICA_TEST_CLAUDE_INVENTORY_RECORD"), record, 0o600); err != nil {
		os.Exit(2)
	}
	switch os.Getenv("MULTICA_TEST_CLAUDE_INVENTORY_MODE") {
	case "hang":
		time.Sleep(time.Hour)
	case "overflow":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", claudePluginInventoryMaxBytes+1))
	case "fail":
		_, _ = io.WriteString(os.Stderr, "fixture inventory error")
		os.Exit(2)
	default:
		_, _ = io.WriteString(os.Stdout, `[{"id":"paper@market","enabled":false,"scope":"user"}]`)
	}
	os.Exit(0)
}

func claudePluginInventoryFixture(t *testing.T, mode string) (Config, ExecOptions, string) {
	t.Helper()
	root := t.TempDir()
	record := filepath.Join(root, "record.json")
	return Config{
		ExecutablePath: os.Args[0],
		LaunchPrefix:   []string{"-test.run=^TestClaudePluginInventoryHelper$", "--"},
		Env: map[string]string{
			"MULTICA_TEST_CLAUDE_INVENTORY_HELPER": "1",
			"MULTICA_TEST_CLAUDE_INVENTORY_RECORD": record,
			"MULTICA_TEST_CLAUDE_INVENTORY_MODE":   mode,
			"CLAUDE_CONFIG_DIR":                    filepath.Join(root, "custom-config"),
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, ExecOptions{Cwd: root, ClaudeSettingsPath: filepath.Join(root, "run-settings.json")}, record
}

func TestQueryClaudePluginsUsesLaunchContext(t *testing.T) {
	t.Parallel()
	cfg, opts, record := claudePluginInventoryFixture(t, "")
	plugins, err := QueryClaudePlugins(context.Background(), cfg, opts)
	if err != nil || len(plugins) != 1 || plugins[0].Enabled {
		t.Fatalf("native policy: %+v, %v", plugins, err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Cwd, Config string
		Args        []string
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	wantDir, err := os.Stat(opts.Cwd)
	if err != nil {
		t.Fatal(err)
	}
	actualDir, err := os.Stat(got.Cwd)
	if err != nil {
		t.Fatal(err)
	}
	// Windows can report an 8.3 path while EvalSymlinks returns its long name.
	if !os.SameFile(actualDir, wantDir) || got.Config != cfg.Env["CLAUDE_CONFIG_DIR"] {
		t.Fatalf("wrong policy context: %+v", got)
	}
	wantArgs := append(append([]string{}, cfg.LaunchPrefix...), "--settings", opts.ClaudeSettingsPath, "plugin", "list", "--json")
	if !reflect.DeepEqual(got.Args, wantArgs) {
		t.Fatalf("argv = %v, want %v", got.Args, wantArgs)
	}
}

func TestQueryClaudePluginsRejectsFailures(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"hang", "overflow", "fail"} {
		t.Run(mode, func(t *testing.T) {
			cfg, opts, _ := claudePluginInventoryFixture(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := QueryClaudePlugins(ctx, cfg, opts); err == nil {
				t.Fatal("inventory failure silently became an empty list")
			}
		})
	}
}

func TestQueryClaudePluginsCancellationDoesNotLaunch(t *testing.T) {
	t.Parallel()
	cfg, opts, record := claudePluginInventoryFixture(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := QueryClaudePlugins(ctx, cfg, opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("cancelled probe launched: %v", err)
	}
}

func TestQueryClaudePluginsRejectsConflictingPrefix(t *testing.T) {
	t.Parallel()
	cfg, opts, record := claudePluginInventoryFixture(t, "")
	cfg.LaunchPrefix = append(cfg.LaunchPrefix, "'--plugin-dir=/unfiltered'")
	if _, err := QueryClaudePlugins(context.Background(), cfg, opts); err == nil {
		t.Fatal("conflicting launch prefix accepted")
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("invalid probe launched: %v", err)
	}
}
