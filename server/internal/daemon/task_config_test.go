package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateTaskConfigRefRejectsUnsafeOrInvalidReferences(t *testing.T) {
	base := taskConfigRef{
		Provider:    "aws_secrets_manager",
		ProviderRef: "arn:aws:secretsmanager:ap-southeast-2:123456789012:secret:backend",
		Version:     "v7",
		Path:        "deploy/terraform/backend.hcl",
		Mode:        0o600,
		Repo:        "repo",
		Target:      "main",
		Account:     "acct",
		Region:      "ap-southeast-2",
	}
	for _, tc := range []struct {
		name string
		ref  taskConfigRef
	}{
		{"absolute", func() taskConfigRef { r := base; r.Path = "/tmp/backend.hcl"; return r }()},
		{"parent traversal", func() taskConfigRef { r := base; r.Path = "deploy/../backend.hcl"; return r }()},
		{"wrong mode", func() taskConfigRef { r := base; r.Mode = 0o644; return r }()},
		{"missing version", func() taskConfigRef { r := base; r.Version = ""; return r }()},
		{"missing provider", func() taskConfigRef { r := base; r.ProviderRef = ""; return r }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateTaskConfigRef(tc.ref); err == nil {
				t.Fatal("validateTaskConfigRef accepted invalid reference")
			}
		})
	}
}

func TestMaterializeTaskConfigIsAtomic0600AndCleansOnFailure(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ref := taskConfigRef{Provider: "aws_secrets_manager", ProviderRef: "ref", Version: "v1", Path: "deploy/terraform/backend.hcl", Mode: 0o600, Repo: "repo", Target: "main", Account: "acct", Region: "ap-southeast-2"}
	secret := []byte("unique-backend-sentinel")

	materialized, err := materializeTaskConfig(context.Background(), "task-1", envRoot, workDir, ref, func(context.Context) ([]byte, error) {
		return append([]byte(nil), secret...), nil
	})
	if err != nil {
		t.Fatalf("materializeTaskConfig: %v", err)
	}
	path := filepath.Join(workDir, ref.Path)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat materialized file: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("materialized mode/type = %s, want regular 0600", info.Mode())
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(secret) {
		t.Fatalf("materialized bytes = %q, err=%v", got, err)
	}
	if err := preflightTaskConfig("task-1", materialized, ref); err != nil {
		t.Fatalf("preflightTaskConfig: %v", err)
	}
	if err := cleanupTaskConfig(materialized); err != nil {
		t.Fatalf("cleanupTaskConfig: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("materialized path after cleanup: err=%v", err)
	}

	// Provider failure must not create a destination or leave a manifest entry.
	ref.Path = "deploy/terraform/failure.hcl"
	_, err = materializeTaskConfig(context.Background(), "task-2", envRoot, workDir, ref, func(context.Context) ([]byte, error) {
		return nil, errors.New("provider failed")
	})
	if err == nil || strings.Contains(err.Error(), string(secret)) {
		t.Fatalf("provider failure = %v, expected redacted error", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ref.Path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed materialization left destination: %v", err)
	}
}

func TestPreflightTaskConfigFailsClosedForIdentityTupleAndCollision(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ref := taskConfigRef{Provider: "aws_secrets_manager", ProviderRef: "ref", Version: "v1", Path: "deploy/terraform/backend.hcl", Mode: 0o600, Repo: "repo", Target: "main", Account: "acct", Region: "ap-southeast-2"}
	m, err := materializeTaskConfig(context.Background(), "task-1", envRoot, workDir, ref, func(context.Context) ([]byte, error) { return []byte("bytes"), nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, taskID := range []string{"task-2", ""} {
		if err := preflightTaskConfig(taskID, m, ref); err == nil {
			t.Errorf("preflight accepted task identity %q", taskID)
		}
	}
	bad := ref
	bad.Path = "deploy/terraform/other.hcl"
	if err := preflightTaskConfig("task-1", m, bad); err == nil {
		t.Error("preflight accepted tuple/path mismatch")
	}
	if err := cleanupTaskConfig(m); err != nil {
		t.Fatal(err)
	}
}

func TestMaterializeTaskConfigRejectsSymlinkAndDestinationCollision(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ref := taskConfigRef{Provider: "aws_secrets_manager", ProviderRef: "ref", Version: "v1", Path: "deploy/terraform/backend.hcl", Mode: 0o600, Repo: "repo", Target: "main", Account: "acct", Region: "ap-southeast-2"}
	if err := os.MkdirAll(filepath.Join(workDir, "deploy", "terraform"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "deploy", "terraform", "backend.hcl"), []byte("user-owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := materializeTaskConfig(context.Background(), "task-1", envRoot, workDir, ref, func(context.Context) ([]byte, error) { return []byte("bytes"), nil }); err == nil {
		t.Fatal("materializer overwrote an existing destination")
	}
	if err := os.Remove(filepath.Join(workDir, "deploy", "terraform", "backend.hcl")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workDir, "deploy")); err == nil {
		t.Fatal("expected symlink setup to replace directory")
	}
	// A symlink parent is rejected even when its target is writable.
	if err := os.RemoveAll(filepath.Join(workDir, "deploy")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(workDir, "deploy")); err != nil {
		t.Fatal(err)
	}
	if _, err := materializeTaskConfig(context.Background(), "task-2", envRoot, workDir, ref, func(context.Context) ([]byte, error) { return []byte("bytes"), nil }); err == nil {
		t.Fatal("materializer followed a symlink parent")
	}
}
