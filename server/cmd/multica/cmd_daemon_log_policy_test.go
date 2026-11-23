package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func clearDaemonLogEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"MULTICA_DAEMON_LOG_MAX_SIZE_MB", "MULTICA_DAEMON_LOG_MAX_BACKUPS", "MULTICA_DAEMON_LOG_MAX_AGE_DAYS"} {
		t.Setenv(key, "")
	}
}

func TestDaemonLogPolicyPrecedence(t *testing.T) {
	clearDaemonLogEnv(t)
	for _, tc := range []struct {
		key      string
		fallback int
		max      int64
		config   func(int) cli.CLIConfig
		read     func(cli.CLIConfig) int
	}{
		{"MULTICA_DAEMON_LOG_MAX_SIZE_MB", 20, maxDaemonLogSizeMB, func(n int) cli.CLIConfig { return cli.CLIConfig{DaemonLogMaxSizeMB: n} }, func(c cli.CLIConfig) int { return newDaemonLogRotator("unused", c).MaxSize }},
		{"MULTICA_DAEMON_LOG_MAX_BACKUPS", 5, maxDaemonLogBackups, func(n int) cli.CLIConfig { return cli.CLIConfig{DaemonLogMaxBackups: n} }, func(c cli.CLIConfig) int { return newDaemonLogRotator("unused", c).MaxBackups }},
		{"MULTICA_DAEMON_LOG_MAX_AGE_DAYS", 30, maxDaemonLogAgeDays, func(n int) cli.CLIConfig { return cli.CLIConfig{DaemonLogMaxAgeDays: n} }, func(c cli.CLIConfig) int { return newDaemonLogRotator("unused", c).MaxAge }},
	} {
		t.Run(tc.key, func(t *testing.T) {
			envValues := []string{"", "   ", "0", "-1", "bad", strings.Repeat("9", 50), "7", "  8 "}
			if tc.max < maxDaemonLogBackups {
				envValues = append(envValues, strconv.FormatInt(tc.max+1, 10))
			}
			for _, saved := range []int{0, 3, -1} {
				for _, value := range envValues {
					t.Run(fmt.Sprintf("saved=%d/env=%q", saved, value), func(t *testing.T) {
						t.Setenv(tc.key, value)
						want := tc.fallback
						if saved > 0 {
							want = saved
						}
						if value == "7" {
							want = 7
						} else if value == "  8 " {
							want = 8
						}
						if got := tc.read(tc.config(saved)); got != want {
							t.Fatalf("got %d, want %d", got, want)
						}
					})
				}
			}
			if tc.max < maxDaemonLogBackups && strconv.IntSize == 64 {
				t.Setenv(tc.key, "")
				if got := tc.read(tc.config(int(tc.max + 1))); got != tc.fallback {
					t.Fatalf("overflowing saved value: got %d, want %d", got, tc.fallback)
				}
			}
		})
	}
}

func TestDaemonLogConfigValidation(t *testing.T) {
	for _, key := range []string{"daemon_log_max_size_mb", "daemon_log_max_backups", "daemon_log_max_age_days"} {
		t.Run(key, func(t *testing.T) {
			for _, invalid := range []string{"0", "-1", "bad", " ", "1.5", strings.Repeat("9", 50)} {
				cfg := cli.CLIConfig{DaemonLogMaxSizeMB: 3, DaemonLogMaxBackups: 3, DaemonLogMaxAgeDays: 3}
				before := cfg
				if err := applyConfigSet(&cfg, key, invalid); err == nil {
					t.Fatalf("accepted %q", invalid)
				}
				if !reflect.DeepEqual(cfg, before) {
					t.Fatalf("rejected value mutated config: %#v", cfg)
				}
			}
		})
	}
	for key, max := range map[string]int64{"daemon_log_max_size_mb": maxDaemonLogSizeMB, "daemon_log_max_age_days": maxDaemonLogAgeDays} {
		cfg := cli.CLIConfig{}
		if err := applyConfigSet(&cfg, key, strconv.FormatInt(max+1, 10)); err == nil {
			t.Fatalf("%s accepted conversion overflow", key)
		}
	}
}

func TestDaemonLogProfilePersistenceAndWriter(t *testing.T) {
	clearDaemonLogEnv(t)
	t.Setenv(cli.TaskConfigRootEnv, t.TempDir())
	command := func(profile string) *cobra.Command {
		c := &cobra.Command{}
		c.Flags().String("profile", profile, "")
		return c
	}
	for _, kv := range [][2]string{{"daemon_log_max_size_mb", "1"}, {"daemon_log_max_backups", "2"}, {"daemon_log_max_age_days", "7"}} {
		if err := runConfigSet(command("small"), kv[:]); err != nil {
			t.Fatal(err)
		}
		if err := runConfigSet(command("other"), []string{kv[0], "9"}); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := cli.LoadCLIConfigForProfile("small")
	if err != nil {
		t.Fatal(err)
	}
	other, err := cli.LoadCLIConfigForProfile("other")
	if err != nil {
		t.Fatal(err)
	}
	if other.DaemonLogMaxSizeMB != 9 || other.DaemonLogMaxBackups != 9 || other.DaemonLogMaxAgeDays != 9 {
		t.Fatalf("other profile changed: %#v", other)
	}
	path := filepath.Join(t.TempDir(), "daemon.log")
	r := newDaemonLogRotator(path, cfg)
	defer r.Close()
	if r.MaxSize != 1 || r.MaxBackups != 2 || r.MaxAge != 7 || !r.Compress {
		t.Fatalf("profile did not reach actual writer: %#v", r)
	}
	payload := bytes.Repeat([]byte("x"), 700*1024)
	// Each write after the first rotates (700 KiB + 700 KiB > 1 MiB), so four
	// writes produce three backups and MaxBackups=2 must prune one.
	for range 4 {
		// lumberjack uses millisecond archive names. Separate real writes so
		// this test exercises retention rather than same-timestamp replacement.
		time.Sleep(2 * time.Millisecond)
		if _, err := r.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	waitForLogFiles(t, func() bool {
		gz, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "daemon-*.log.gz"))
		raw, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "daemon-*.log"))
		return len(gz) == 2 && len(raw) == 0
	})
	gz, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "daemon-*.log.gz"))
	for _, p := range gz {
		if got := readGzipLog(t, p); !bytes.Equal(got, payload) {
			t.Fatalf("archive %s contents changed: %d bytes", p, len(got))
		}
	}
	active, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(active, payload) {
		t.Fatalf("active log not bounded/intact: %d bytes, %v", len(active), err)
	}
	for _, key := range []string{"daemon_log_max_size_mb", "daemon_log_max_backups", "daemon_log_max_age_days"} {
		if err := runConfigSet(command("small"), []string{key, ""}); err != nil {
			t.Fatal(err)
		}
	}
	cleared, err := cli.LoadCLIConfigForProfile("small")
	if err != nil {
		t.Fatal(err)
	}
	r = newDaemonLogRotator(path, cleared)
	if r.MaxSize != 20 || r.MaxBackups != 5 || r.MaxAge != 30 {
		t.Fatalf("clear did not restore defaults: %#v", r)
	}
}

func TestDaemonLogProfileAgeRetention(t *testing.T) {
	clearDaemonLogEnv(t)
	dir := t.TempDir()
	name := func(age time.Duration) string {
		return filepath.Join(dir, "daemon-"+time.Now().UTC().Add(-age).Format("2006-01-02T15-04-05.000")+".log")
	}
	old, recent := name(72*time.Hour), name(12*time.Hour)
	for _, p := range []string{old, recent} {
		if err := os.WriteFile(p, []byte("synthetic retained log"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := newDaemonLogRotator(filepath.Join(dir, "daemon.log"), cli.CLIConfig{DaemonLogMaxSizeMB: 1, DaemonLogMaxBackups: 5, DaemonLogMaxAgeDays: 1})
	defer r.Close()
	if _, err := r.Write([]byte("current")); err != nil {
		t.Fatal(err)
	}
	if err := r.Rotate(); err != nil {
		t.Fatal(err)
	}
	waitForLogFiles(t, func() bool {
		_, oldErr := os.Stat(old)
		_, recentErr := os.Stat(recent)
		_, gzErr := os.Stat(recent + ".gz")
		return os.IsNotExist(oldErr) && os.IsNotExist(recentErr) && gzErr == nil
	})
	if _, err := os.Stat(old + ".gz"); !os.IsNotExist(err) {
		t.Fatalf("expired backup was compressed/retained: %v", err)
	}
	if got := readGzipLog(t, recent+".gz"); string(got) != "synthetic retained log" {
		t.Fatalf("recent backup changed: %q", got)
	}
}

func waitForLogFiles(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case <-deadline.C:
			t.Fatal("lumberjack did not finish asynchronous retention/compression")
		case <-tick.C:
		}
	}
}

func readGzipLog(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return b
}
