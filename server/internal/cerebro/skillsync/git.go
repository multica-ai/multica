package skillsync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// repo is a thin wrapper over a persistent git checkout of the archive that is
// refreshed and reused across ticks.
type repo struct {
	cfg Config
}

// authRemote injects the token into the https remote URL. Never log the
// result — scrubToken sanitises any error that might echo it.
func (r *repo) authRemote() string {
	u := r.cfg.Repo
	if strings.HasPrefix(u, "https://") {
		return "https://x-access-token:" + r.cfg.Token + "@" + strings.TrimPrefix(u, "https://")
	}
	return u
}

func (r *repo) scrubToken(s string) string {
	if r.cfg.Token == "" {
		return s
	}
	return strings.ReplaceAll(s, r.cfg.Token, "***")
}

// runGit runs a git command in the checkout directory and returns trimmed
// stdout. Errors carry stderr with the token scrubbed.
func (r *repo) runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	// Deterministic, non-interactive: no credential prompts, no pager, and
	// the configured identity stamps both author and committer.
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME="+r.cfg.AuthorName,
		"GIT_AUTHOR_EMAIL="+r.cfg.AuthorEmail,
		"GIT_COMMITTER_NAME="+r.cfg.AuthorName,
		"GIT_COMMITTER_EMAIL="+r.cfg.AuthorEmail,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, r.scrubToken(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ensureCheckout makes the work dir a clean checkout of <branch> at the remote
// tip. It clones on first run and otherwise fetches and hard-resets, discarding
// any leftover state from a previous tick.
func (r *repo) ensureCheckout(ctx context.Context) (string, error) {
	dir := r.cfg.WorkDir
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		if err := os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("clear work dir: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return "", fmt.Errorf("create work dir parent: %w", err)
		}
		if _, err := r.runGit(ctx, "", "clone", "--branch", r.cfg.Branch, "--single-branch", r.authRemote(), dir); err != nil {
			return "", err
		}
		return dir, nil
	}
	// Refresh the existing checkout. Re-point the remote each time so a
	// rotated token takes effect without a re-clone.
	if _, err := r.runGit(ctx, dir, "remote", "set-url", "origin", r.authRemote()); err != nil {
		return "", err
	}
	if _, err := r.runGit(ctx, dir, "fetch", "--depth", "1", "origin", r.cfg.Branch); err != nil {
		return "", err
	}
	if _, err := r.runGit(ctx, dir, "checkout", r.cfg.Branch); err != nil {
		return "", err
	}
	if _, err := r.runGit(ctx, dir, "reset", "--hard", "origin/"+r.cfg.Branch); err != nil {
		return "", err
	}
	if _, err := r.runGit(ctx, dir, "clean", "-fd"); err != nil {
		return "", err
	}
	return dir, nil
}

// existingSlugDomains maps each skill slug already in the archive to the domain
// folder it lives in, so the sync can update a skill in place rather than
// re-homing it and disturbing the curated layout.
func existingSlugDomains(dir string) (map[string]string, error) {
	out := make(map[string]string)
	for domain := range archiveDomains {
		domainDir := filepath.Join(dir, domain)
		entries, err := os.ReadDir(domainDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(domainDir, e.Name(), "SKILL.md")); err == nil {
				out[e.Name()] = domain
			}
		}
	}
	return out, nil
}

// writeSkillFiles writes a skill's SKILL.md and supporting files under
// <dir>/<domain>/<slug>/. It overwrites the files the DB knows about but never
// prunes — the archive may legitimately carry extra curated files (LICENSE,
// vendored assets) that no longer round-trip through the DB.
func writeSkillFiles(dir, domain, slug string, files map[string]string) error {
	base := filepath.Join(dir, domain, slug)
	for rel, content := range files {
		clean := filepath.Clean(rel)
		if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			// Defend against a path that would escape the skill dir.
			continue
		}
		full := filepath.Join(base, clean)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// commitAndPush stages everything, and commits + pushes only when the working
// tree actually changed. Returns whether a commit was pushed. This is what
// makes the nightly run idempotent: an unchanged archive produces no commit.
func (r *repo) commitAndPush(ctx context.Context, dir, message string) (bool, error) {
	if _, err := r.runGit(ctx, dir, "add", "-A"); err != nil {
		return false, err
	}
	status, err := r.runGit(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if status == "" {
		return false, nil
	}
	if _, err := r.runGit(ctx, dir, "commit", "-m", message); err != nil {
		return false, err
	}
	if _, err := r.runGit(ctx, dir, "push", "origin", "HEAD:"+r.cfg.Branch); err != nil {
		return false, err
	}
	return true, nil
}

// gitAvailable reports whether a git binary is on PATH. The runtime container
// ships git for this feature; a self-host image without it disables the worker
// gracefully rather than erroring every tick.
func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
