package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexThreadSetupChecksManagedInstructionBudget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bytes     int
		limit     int
		blocked   bool
		resume    bool
		unmanaged bool
	}{
		{"default_small", 2048, 32768, false, false, false},
		{"default_overflow", 50000, 32768, true, false, false},
		{"explicit_larger_limit", 50000, 65536, false, false, false},
		{"explicit_smaller_limit", 6000, 4096, true, false, false},
		{"resume_within_limit", 2048, 32768, false, true, false},
		{"unmanaged_call", 50000, 32768, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			path := filepath.Join(cwd, "AGENTS.md")
			original := strings.Repeat("x", tc.bytes)
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			c, _, _ := newTestCodexClient(t)
			stdin := &fakeStdinWithHook{}
			var methods []string
			stdin.afterWrite = func() {
				lines := stdin.Lines()
				var request struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				if err := json.Unmarshal([]byte(lines[len(lines)-1]), &request); err != nil {
					t.Fatal(err)
				}
				methods = append(methods, request.Method)
				result := fmt.Sprintf(`{"config":{"project_doc_max_bytes":%d},"layers":[]}`, tc.limit)
				if request.Method == "thread/start" || request.Method == "thread/resume" {
					result = `{"thread":{"id":"thread-synthetic"}}`
				}
				c.handleLine(fmt.Sprintf(`{"id":%d,"result":%s}`, request.ID, result))
			}
			c.stdin = stdin
			opts := ExecOptions{Cwd: cwd, CodexInstructionsPath: path}
			if tc.resume {
				opts.ResumeSessionID = "thread-synthetic"
			}
			if tc.unmanaged {
				opts.CodexInstructionsPath = ""
			}
			thread, resumed, err := c.startOrResumeThread(t.Context(), opts, slog.Default())
			if tc.blocked {
				if err == nil {
					t.Fatalf("started %q with %d instruction bytes and a %d-byte budget", thread, tc.bytes, tc.limit)
				}
				for _, method := range methods {
					if method == "thread/start" || method == "thread/resume" {
						t.Fatalf("unsafe thread setup sent: %v", methods)
					}
				}
				if !strings.Contains(err.Error(), fmt.Sprint(tc.limit)) {
					t.Fatalf("error omits effective limit: %v", err)
				}
			} else if err != nil || thread != "thread-synthetic" || resumed != tc.resume {
				t.Fatalf("valid managed instructions rejected: thread=%q err=%v", thread, err)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || string(after) != original {
				t.Fatal("preflight changed repository instructions")
			}
		})
	}
}

func TestCodexProjectInstructionDiscoveryBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limit      int64
		parent     string
		override   *string
		fallback   string
		body       string
		rootMarker string
		markers    *[]string
		blocked    bool
	}{
		{name: "exact_limit", limit: 8, body: "12345678"},
		{name: "one_byte_over", limit: 8, body: "123456789", blocked: true},
		{name: "utf8_exact", limit: 6, body: "界界"},
		{name: "utf8_over", limit: 5, body: "界界", blocked: true},
		{name: "ancestor_exact", limit: 8, parent: "1234", body: "5678", rootMarker: ".git"},
		{name: "ancestor_exhausts", limit: 8, parent: "12345678", body: "9", rootMarker: ".git", blocked: true},
		{name: "ancestor_truncated", limit: 8, parent: "123456789", body: "x", rootMarker: ".git", blocked: true},
		{name: "whitespace_ancestor", limit: 8, parent: " \n \t", body: "12345678", rootMarker: ".git"},
		{name: "oversized_whitespace_prefix", limit: 8, parent: strings.Repeat(" ", 20), body: "12345678", rootMarker: ".git"},
		{name: "no_root_ignores_parent", limit: 8, parent: "123456789", body: "12345678"},
		{name: "disabled_parent_walk", limit: 8, parent: "123456789", body: "12345678", rootMarker: ".git", markers: &[]string{}},
		{name: "custom_root", limit: 8, parent: "12345678", body: "9", rootMarker: "PROJECT", markers: &[]string{"PROJECT"}, blocked: true},
		{name: "fallback_ancestor", limit: 8, fallback: "12345678", body: "9", rootMarker: ".git", blocked: true},
		{name: "ancestor_override", limit: 8, parent: "123456789", override: testCodexString("1"), body: "2345678", rootMarker: ".git"},
		{name: "empty_override_still_wins", limit: 8, parent: "123456789", override: testCodexString(""), body: "12345678", rootMarker: ".git"},
		{name: "zero_budget", limit: 0, body: "x", blocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cwd := filepath.Join(root, "child")
			if err := os.Mkdir(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			write := func(path, body string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.rootMarker != "" {
				write(filepath.Join(root, tc.rootMarker), "")
			}
			if tc.parent != "" {
				write(filepath.Join(root, "AGENTS.md"), tc.parent)
			}
			if tc.override != nil {
				write(filepath.Join(root, "AGENTS.override.md"), *tc.override)
			}
			if tc.fallback != "" {
				write(filepath.Join(root, "TEAM.md"), tc.fallback)
			}
			managed := filepath.Join(cwd, "AGENTS.md")
			write(managed, tc.body)
			err := checkCodexInstructionFiles(cwd, managed, codexInstructionConfig{MaxBytes: &tc.limit, RootMarkers: tc.markers, FallbackNames: []string{"TEAM.md"}})
			if (err != nil) != tc.blocked {
				t.Fatalf("blocked=%v want %v: %v", err != nil, tc.blocked, err)
			}
		})
	}
}

func TestCodexOverrideDoesNotSilentlyHideManagedInstructions(t *testing.T) {
	cwd := t.TempDir()
	managed := filepath.Join(cwd, "AGENTS.md")
	if err := os.WriteFile(managed, []byte("SYNTHETIC_PRIVATE_BODY"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.override.md"), []byte("repository override"), 0600); err != nil {
		t.Fatal(err)
	}
	err := checkCodexInstructionFiles(cwd, managed, codexInstructionConfig{})
	if err == nil || !strings.Contains(err.Error(), "not selected") || strings.Contains(err.Error(), "SYNTHETIC_PRIVATE_BODY") {
		t.Fatalf("shadowed instructions not diagnosed safely: %v", err)
	}
}

func testCodexString(value string) *string { return &value }

func TestCodexInstructionPreflightUsesNativeConfigurationScopes(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "child")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{".git": "", "AGENTS.md": strings.Repeat("p", 100)} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(cwd, "AGENTS.md")
	if err := os.WriteFile(path, []byte("managed"), 0600); err != nil {
		t.Fatal(err)
	}
	c, _, _ := newTestCodexClient(t)
	stdin := &fakeStdinWithHook{}
	var configs []string
	stdin.afterWrite = func() {
		lines := stdin.Lines()
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Cwd string `json:"cwd"`
			} `json:"params"`
		}
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &request); err != nil {
			t.Fatal(err)
		}
		result := `{"thread":{"id":"thread-synthetic"}}`
		if request.Method == "config/read" {
			configs = append(configs, request.Params.Cwd)
			// Only the non-project scope disables parent traversal. A guessed
			// default or using the project layer's marker would reject this run.
			result = `{"config":{"project_doc_max_bytes":0,"project_root_markers":[]},"layers":[]}`
			if request.Params.Cwd != "" {
				result = `{"config":{"project_doc_max_bytes":7,"project_root_markers":[".git"]},"layers":[]}`
			}
		}
		c.handleLine(fmt.Sprintf(`{"id":%d,"result":%s}`, request.ID, result))
	}
	c.stdin = stdin
	thread, _, err := c.startOrResumeThread(t.Context(), ExecOptions{Cwd: cwd, CodexInstructionsPath: path}, slog.Default())
	if err != nil || thread != "thread-synthetic" {
		t.Fatalf("native scopes not respected: %q %v", thread, err)
	}
	if len(configs) != 2 || configs[0] != "" || configs[1] != cwd {
		t.Fatalf("configuration scopes=%v", configs)
	}
}

func TestCodexInstructionPreflightFailureDoesNotExposeNativeData(t *testing.T) {
	for _, response := range []string{
		`{"error":{"code":-32603,"message":"SYNTHETIC_PRIVATE_CONFIGURATION"}}`,
		`{"result":{}}`,
		`{"result":{"config":{"project_doc_max_bytes":"SYNTHETIC_PRIVATE_CONFIGURATION"}}}`,
	} {
		t.Run(response, func(t *testing.T) {
			c, _, _ := newTestCodexClient(t)
			stdin := &fakeStdinWithHook{}
			stdin.afterWrite = func() {
				lines := stdin.Lines()
				var request struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				if err := json.Unmarshal([]byte(lines[len(lines)-1]), &request); err != nil {
					t.Fatal(err)
				}
				if request.Method != "config/read" {
					t.Fatalf("thread setup sent after unknown configuration: %s", request.Method)
				}
				c.handleLine(fmt.Sprintf(`{"id":%d,%s`, request.ID, response[1:]))
			}
			c.stdin = stdin
			_, _, err := c.startOrResumeThread(t.Context(), ExecOptions{Cwd: t.TempDir(), CodexInstructionsPath: "managed"}, slog.Default())
			if err == nil || strings.Contains(err.Error(), "SYNTHETIC_PRIVATE_CONFIGURATION") {
				t.Fatalf("unsafe configuration diagnostic: %v", err)
			}
		})
	}
}

func TestCodexInstructionPreflightBlocksResumeBeforeRPC(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "AGENTS.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 50000)), 0600); err != nil {
		t.Fatal(err)
	}
	c, _, _ := newTestCodexClient(t)
	stdin := &fakeStdinWithHook{}
	stdin.afterWrite = func() {
		lines := stdin.Lines()
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &request); err != nil {
			t.Fatal(err)
		}
		if request.Method != "config/read" {
			t.Fatalf("unsafe resume sent: %s", request.Method)
		}
		c.handleLine(fmt.Sprintf(`{"id":%d,"result":{"config":{},"layers":[]}}`, request.ID))
	}
	c.stdin = stdin
	_, resumed, err := c.startOrResumeThread(t.Context(), ExecOptions{Cwd: cwd, CodexInstructionsPath: path, ResumeSessionID: "existing-thread"}, slog.Default())
	if err == nil || resumed || !strings.Contains(err.Error(), "32768") {
		t.Fatalf("unsafe resume: resumed=%v err=%v", resumed, err)
	}
}

func TestCodexInstructionConfigReadHasHandshakeDeadline(t *testing.T) {
	c, _, _ := newTestCodexClient(t)
	c.handshakeTimeout = time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err := c.request(ctx, "config/read", map[string]any{"includeLayers": false})
	var timeout *codexHandshakeTimeoutError
	if !errors.As(err, &timeout) || timeout.Method != "config/read" {
		t.Fatalf("config read did not use its handshake deadline: %v", err)
	}
}

func TestCodexInstructionPreflightPreservesCancellation(t *testing.T) {
	c, stdin, _ := newTestCodexClient(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := c.checkManagedInstructions(ctx, ExecOptions{Cwd: t.TempDir(), CodexInstructionsPath: "managed"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation replaced by configuration error: %v", err)
	}
	if len(stdin.Lines()) != 0 {
		t.Fatal("cancelled preflight sent an RPC")
	}
}

func TestCodexInstructionPreflightRejectsActivatingProjectConfig(t *testing.T) {
	for _, tc := range []struct {
		name, disabled, higher string
		blocked                bool
		resume                 bool
	}{
		{"smaller_budget", `{"project_doc_max_bytes":4}`, `{}`, true, false},
		{"resume_could_fall_back_to_fresh", `{"project_doc_max_bytes":4}`, `{}`, true, true},
		{"fallback_names", `{"project_doc_fallback_filenames":["TEAM.md"]}`, `{}`, true, false},
		{"unrelated_disabled_setting", `{"model_reasoning_effort":"high"}`, `{}`, false, true},
		{"cli_budget_override", `{"project_doc_max_bytes":4}`, `{"project_doc_max_bytes":8}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			path := filepath.Join(cwd, "AGENTS.md")
			if err := os.WriteFile(path, []byte("12345"), 0600); err != nil {
				t.Fatal(err)
			}
			c, _, _ := newTestCodexClient(t)
			stdin := &fakeStdinWithHook{}
			var setup bool
			stdin.afterWrite = func() {
				lines := stdin.Lines()
				var req struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
					Params struct {
						IncludeLayers bool `json:"includeLayers"`
					} `json:"params"`
				}
				if err := json.Unmarshal([]byte(lines[len(lines)-1]), &req); err != nil {
					t.Fatal(err)
				}
				result := `{"thread":{"id":"thread-synthetic"}}`
				if req.Method == "config/read" {
					result = fmt.Sprintf(`{"config":{"project_doc_max_bytes":8},"layers":[{"name":{"type":"sessionFlags"},"config":%s},{"name":{"type":"project","dotCodexFolder":%q},"disabledReason":"SYNTHETIC_PRIVATE_REASON","config":%s}]}`, tc.higher, filepath.Join(cwd, ".codex"), tc.disabled)
				} else {
					setup = true
				}
				c.handleLine(fmt.Sprintf(`{"id":%d,"result":%s}`, req.ID, result))
			}
			c.stdin = stdin
			opts := ExecOptions{Cwd: cwd, CodexInstructionsPath: path}
			if tc.resume {
				opts.ResumeSessionID = "thread-synthetic"
			}
			_, _, err := c.startOrResumeThread(t.Context(), opts, slog.Default())
			if (err != nil) != tc.blocked || (tc.blocked && setup) {
				t.Fatalf("blocked=%v setup=%v want blocked=%v: %v", err != nil, setup, tc.blocked, err)
			}
			if err != nil && strings.Contains(err.Error(), "SYNTHETIC_PRIVATE_REASON") {
				t.Fatal("native diagnostic leaked")
			}
		})
	}
}

func TestCodexInstructionPreflightHonorsActiveProjectTrust(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		cwdTrust, rootTrust                  string
		unrelated, worktree, bogus, emptyCwd bool
		blocked                              bool
	}{
		{name: "cwd_untrusted", cwdTrust: "untrusted", blocked: true},
		{name: "repository_untrusted", rootTrust: "untrusted", blocked: true},
		{name: "cwd_trusted_overrides_repository", cwdTrust: "trusted", rootTrust: "untrusted"},
		{name: "cwd_without_decision_overrides_repository", rootTrust: "untrusted", emptyCwd: true},
		{name: "unrelated_untrusted", rootTrust: "untrusted", unrelated: true},
		{name: "registered_worktree_inherits_main", rootTrust: "untrusted", worktree: true, blocked: true},
		{name: "bogus_git_directory_is_not_trust_root", rootTrust: "untrusted", bogus: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cwd := filepath.Join(root, "child")
			if err := os.Mkdir(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			dot := filepath.Join(root, ".git")
			if err := os.Mkdir(dot, 0700); err != nil {
				t.Fatal(err)
			}
			write := func(path, body string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if !tc.bogus {
				write(filepath.Join(dot, "HEAD"), "ref: refs/heads/main\n")
			}
			if tc.worktree {
				cwd = t.TempDir()
				meta := filepath.Join(dot, "worktrees", "synthetic")
				if err := os.MkdirAll(meta, 0700); err != nil {
					t.Fatal(err)
				}
				write(filepath.Join(cwd, ".git"), "gitdir: "+meta+"\n")
				write(filepath.Join(meta, "gitdir"), filepath.Join(cwd, ".git")+"\n")
				write(filepath.Join(meta, "commondir"), "../..\n")
			}
			projects := map[string]any{}
			if tc.rootTrust != "" {
				key := root
				if tc.unrelated {
					key = t.TempDir()
				}
				projects[key] = map[string]any{"trust_level": tc.rootTrust}
			}
			if tc.cwdTrust != "" || tc.emptyCwd {
				v := map[string]any{}
				if tc.cwdTrust != "" {
					v["trust_level"] = tc.cwdTrust
				}
				projects[cwd] = v
			}
			config, _ := json.Marshal(map[string]any{"projects": projects})
			path := filepath.Join(cwd, "AGENTS.md")
			write(path, "managed")
			c, _, _ := newTestCodexClient(t)
			stdin := &fakeStdinWithHook{}
			var setup bool
			stdin.afterWrite = func() {
				lines := stdin.Lines()
				var req struct {
					ID     int    `json:"id"`
					Method string `json:"method"`
				}
				if err := json.Unmarshal([]byte(lines[len(lines)-1]), &req); err != nil {
					t.Fatal(err)
				}
				result := `{"thread":{"id":"thread-synthetic"}}`
				if req.Method == "config/read" {
					result = `{"config":` + string(config) + `,"layers":[]}`
				} else {
					setup = true
				}
				c.handleLine(fmt.Sprintf(`{"id":%d,"result":%s}`, req.ID, result))
			}
			c.stdin = stdin
			_, _, err := c.startOrResumeThread(t.Context(), ExecOptions{Cwd: cwd, CodexInstructionsPath: path}, slog.Default())
			if (err != nil) != tc.blocked || (tc.blocked && setup) {
				t.Fatalf("blocked=%v setup=%v want blocked=%v: %v", err != nil, setup, tc.blocked, err)
			}
		})
	}
}

func TestCodexInstructionDiscoveryNormalizesNativeNames(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		t.Run(fmt.Sprint(absolute), func(t *testing.T) {
			root := t.TempDir()
			cwd := filepath.Join(root, "child")
			if err := os.Mkdir(cwd, 0700); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{".git": "", "TEAM.md": "1234", "AGENTS.md": "1234"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(cwd, "AGENTS.md")
			if err := os.WriteFile(path, []byte("1234"), 0600); err != nil {
				t.Fatal(err)
			}
			limit := int64(4)
			config := codexInstructionConfig{MaxBytes: &limit, FallbackNames: []string{"\u2003 TEAM.md \u2003"}}
			if absolute {
				marker := filepath.Join(t.TempDir(), "MARKER")
				if err := os.WriteFile(marker, nil, 0600); err != nil {
					t.Fatal(err)
				}
				config.RootMarkers = &[]string{marker, ".git"}
			} else {
				if err := os.Remove(filepath.Join(root, "AGENTS.md")); err != nil {
					t.Fatal(err)
				}
			}
			err := checkCodexInstructionFiles(cwd, path, config)
			if absolute && err != nil {
				t.Fatalf("native absolute marker should select cwd: %v", err)
			}
			if !absolute && err == nil {
				t.Fatal("trimmed ancestor fallback should exhaust budget")
			}
		})
	}
}
