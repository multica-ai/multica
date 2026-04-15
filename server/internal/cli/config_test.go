package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCLIConfigLoadLegacyWatchedWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	profile := "legacy"
	path, err := CLIConfigPathForProfile(profile)
	if err != nil {
		t.Fatalf("CLIConfigPathForProfile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	legacyJSON := []byte(`{
  "watched_workspaces": [
    {
      "id": "ws-1",
      "name": "Workspace One"
    }
  ]
}`)
	if err := os.WriteFile(path, legacyJSON, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadCLIConfigForProfile(profile)
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if len(cfg.WatchedWorkspaces) != 1 {
		t.Fatalf("expected 1 watched workspace, got %d", len(cfg.WatchedWorkspaces))
	}

	ws := cfg.WatchedWorkspaces[0]
	if ws.ID != "ws-1" {
		t.Fatalf("expected workspace ID ws-1, got %s", ws.ID)
	}
	if ws.Name != "Workspace One" {
		t.Fatalf("expected workspace name Workspace One, got %s", ws.Name)
	}
	if ws.SkillSync != nil {
		t.Fatalf("expected nil SkillSync for legacy config, got %+v", ws.SkillSync)
	}
}

func TestCLIConfigLegacyWatchedWorkspaceRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	profile := "legacy-round-trip"
	path, err := CLIConfigPathForProfile(profile)
	if err != nil {
		t.Fatalf("CLIConfigPathForProfile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	legacyJSON := []byte(`{
  "watched_workspaces": [
    {
      "id": "ws-1",
      "name": "Workspace One"
    }
  ]
}`)
	if err := os.WriteFile(path, legacyJSON, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadCLIConfigForProfile(profile)
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if err := SaveCLIConfigForProfile(cfg, profile); err != nil {
		t.Fatalf("SaveCLIConfigForProfile: %v", err)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(saved, &persisted); err != nil {
		t.Fatalf("json.Unmarshal persisted config: %v", err)
	}
	workspaces, ok := persisted["watched_workspaces"].([]any)
	if !ok {
		t.Fatalf("expected watched_workspaces array in persisted config, got %T", persisted["watched_workspaces"])
	}
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 watched workspace in persisted config, got %d", len(workspaces))
	}
	workspace, ok := workspaces[0].(map[string]any)
	if !ok {
		t.Fatalf("expected watched workspace object, got %T", workspaces[0])
	}
	if _, ok := workspace["skill_sync"]; ok {
		t.Fatalf("expected legacy workspace to persist without skill_sync, got %v", workspace["skill_sync"])
	}

	reloaded, err := LoadCLIConfigForProfile(profile)
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile reload: %v", err)
	}
	if len(reloaded.WatchedWorkspaces) != 1 {
		t.Fatalf("expected 1 watched workspace after reload, got %d", len(reloaded.WatchedWorkspaces))
	}
	if reloaded.WatchedWorkspaces[0].ID != "ws-1" {
		t.Fatalf("expected workspace ID ws-1 after reload, got %s", reloaded.WatchedWorkspaces[0].ID)
	}
	if reloaded.WatchedWorkspaces[0].SkillSync != nil {
		t.Fatalf("expected nil SkillSync after reload, got %+v", reloaded.WatchedWorkspaces[0].SkillSync)
	}
}

func TestCLIConfigRoundTripWithWorkspaceSkillSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	profile := "current"
	want := CLIConfig{
		ServerURL:   "https://api.example.com",
		AppURL:      "https://app.example.com",
		WorkspaceID: "ws-2",
		Token:       "token-123",
		WatchedWorkspaces: []WatchedWorkspace{
			{
				ID:   "ws-2",
				Name: "Workspace Two",
				SkillSync: &WorkspaceSkillSync{
					Dir:           "C:\\skills",
					Enabled:       true,
					DeleteManaged: true,
					LastSyncAt:    "2026-04-15T10:20:30Z",
					LastSyncError: "previous sync failed",
				},
			},
		},
	}

	if err := SaveCLIConfigForProfile(want, profile); err != nil {
		t.Fatalf("SaveCLIConfigForProfile: %v", err)
	}

	got, err := LoadCLIConfigForProfile(profile)
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}

	if got.ServerURL != want.ServerURL {
		t.Fatalf("expected ServerURL %s, got %s", want.ServerURL, got.ServerURL)
	}
	if got.AppURL != want.AppURL {
		t.Fatalf("expected AppURL %s, got %s", want.AppURL, got.AppURL)
	}
	if got.WorkspaceID != want.WorkspaceID {
		t.Fatalf("expected WorkspaceID %s, got %s", want.WorkspaceID, got.WorkspaceID)
	}
	if got.Token != want.Token {
		t.Fatalf("expected Token %s, got %s", want.Token, got.Token)
	}
	if len(got.WatchedWorkspaces) != 1 {
		t.Fatalf("expected 1 watched workspace, got %d", len(got.WatchedWorkspaces))
	}

	ws := got.WatchedWorkspaces[0]
	if ws.ID != want.WatchedWorkspaces[0].ID || ws.Name != want.WatchedWorkspaces[0].Name {
		t.Fatalf("unexpected watched workspace: %+v", ws)
	}
	if ws.SkillSync == nil {
		t.Fatal("expected SkillSync to round-trip, got nil")
	}
	if *ws.SkillSync != *want.WatchedWorkspaces[0].SkillSync {
		t.Fatalf("unexpected SkillSync: got %+v want %+v", ws.SkillSync, want.WatchedWorkspaces[0].SkillSync)
	}
}

func TestUpdateCLIConfigForProfileSerializesConcurrentMutations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	profile := "concurrent-updates"
	if err := SaveCLIConfigForProfile(CLIConfig{}, profile); err != nil {
		t.Fatalf("SaveCLIConfigForProfile: %v", err)
	}

	const writers = 12
	var wg sync.WaitGroup
	wg.Add(writers)

	errCh := make(chan error, writers)
	for i := 0; i < writers; i++ {
		i := i
		go func() {
			defer wg.Done()
			err := UpdateCLIConfigForProfile(profile, func(cfg *CLIConfig) error {
				id := fmt.Sprintf("ws-%d", i)
				cfg.AddWatchedWorkspace(id, "Workspace "+id)
				return nil
			})
			errCh <- err
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("UpdateCLIConfigForProfile returned error: %v", err)
		}
	}

	cfg, err := LoadCLIConfigForProfile(profile)
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if len(cfg.WatchedWorkspaces) != writers {
		t.Fatalf("expected %d watched workspaces, got %d", writers, len(cfg.WatchedWorkspaces))
	}

	seen := make(map[string]bool, writers)
	for _, ws := range cfg.WatchedWorkspaces {
		seen[ws.ID] = true
	}
	for i := 0; i < writers; i++ {
		id := fmt.Sprintf("ws-%d", i)
		if !seen[id] {
			t.Fatalf("missing watched workspace %s after concurrent updates", id)
		}
	}
}

func TestUpdateCLIConfigForProfileWaitsForLockFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	profile := "lock-wait"
	if err := SaveCLIConfigForProfile(CLIConfig{}, profile); err != nil {
		t.Fatalf("SaveCLIConfigForProfile: %v", err)
	}

	lockPath, err := cliConfigLockPathForProfile(profile)
	if err != nil {
		t.Fatalf("cliConfigLockPathForProfile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("held"), 0o600); err != nil {
		t.Fatalf("WriteFile lock: %v", err)
	}

	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.Remove(lockPath)
	}()

	start := time.Now()
	if err := UpdateCLIConfigForProfile(profile, func(cfg *CLIConfig) error {
		cfg.ServerURL = "https://example.com"
		return nil
	}); err != nil {
		t.Fatalf("UpdateCLIConfigForProfile: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("expected update to wait for lock release, elapsed=%s", elapsed)
	}

	cfg, err := LoadCLIConfigForProfile(profile)
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if cfg.ServerURL != "https://example.com" {
		t.Fatalf("ServerURL = %q, want https://example.com", cfg.ServerURL)
	}
}

func TestUpdateCLIConfigForProfileRemovesStaleLockFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	profile := "stale-lock"
	if err := SaveCLIConfigForProfile(CLIConfig{}, profile); err != nil {
		t.Fatalf("SaveCLIConfigForProfile: %v", err)
	}

	lockPath, err := cliConfigLockPathForProfile(profile)
	if err != nil {
		t.Fatalf("cliConfigLockPathForProfile: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
		t.Fatalf("WriteFile lock: %v", err)
	}
	staleTime := time.Now().Add(-cliConfigLockStaleAfter - time.Second)
	if err := os.Chtimes(lockPath, staleTime, staleTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if err := UpdateCLIConfigForProfile(profile, func(cfg *CLIConfig) error {
		cfg.AppURL = "https://app.example.com"
		return nil
	}); err != nil {
		t.Fatalf("UpdateCLIConfigForProfile: %v", err)
	}

	cfg, err := LoadCLIConfigForProfile(profile)
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if cfg.AppURL != "https://app.example.com" {
		t.Fatalf("AppURL = %q, want https://app.example.com", cfg.AppURL)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("expected lock file to be removed, stat err=%v", err)
	}
}
