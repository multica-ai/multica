package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type DaemonCommand struct {
	ID          string         `json:"id"`
	CommandType string         `json:"command_type"`
	Payload     map[string]any `json:"payload"`
}

func resolveIntelliJCommand(cfg Config) string {
	if v := strings.TrimSpace(os.Getenv("MULTICA_INTELLIJ_COMMAND")); v != "" {
		return v
	}
	if v := strings.TrimSpace(cfg.IntelliJCommand); v != "" {
		return v
	}
	if path, err := lookPath("idea"); err == nil {
		return path
	}
	if path, err := lookPath("idea64.exe"); err == nil {
		return path
	}
	if path, err := lookPath("idea.exe"); err == nil {
		return path
	}
	return "idea"
}

func openIntelliJ(ctx context.Context, workDir string, command string, branchName string) error {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return errors.New("work_dir is empty")
	}
	st, err := os.Stat(workDir)
	if err != nil {
		return fmt.Errorf("stat work_dir: %w", err)
	}
	if !st.IsDir() {
		return errors.New("work_dir is not a directory")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return errors.New("IntelliJ IDEA command is empty")
	}
	if err := ensureGitWorktreeBranch(ctx, workDir, branchName); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, command, workDir)
	return cmd.Start()
}

func ensureGitWorktreeBranch(ctx context.Context, workDir string, branchName string) error {
	branchName = strings.TrimSpace(branchName)
	if branchName == "" {
		return nil
	}

	checkRef := exec.CommandContext(ctx, "git", "check-ref-format", "--branch", branchName)
	if out, err := checkRef.CombinedOutput(); err != nil {
		return fmt.Errorf("invalid branch_name %q: %s: %w", branchName, strings.TrimSpace(string(out)), err)
	}

	insideCmd := exec.CommandContext(ctx, "git", "-C", workDir, "rev-parse", "--is-inside-work-tree")
	out, err := insideCmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return nil
	}

	currentCmd := exec.CommandContext(ctx, "git", "-C", workDir, "branch", "--show-current")
	out, err = currentCmd.Output()
	if err != nil {
		return fmt.Errorf("git branch --show-current: %w", err)
	}
	if strings.TrimSpace(string(out)) == branchName {
		return nil
	}

	checkoutCmd := exec.CommandContext(ctx, "git", "-C", workDir, "checkout", branchName)
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		createCmd := exec.CommandContext(ctx, "git", "-C", workDir, "checkout", "-b", branchName)
		if _, createErr := createCmd.CombinedOutput(); createErr != nil {
			return fmt.Errorf("git checkout %s: %s: %w", branchName, strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

func (d *Daemon) commandLoop(ctx context.Context) {
	interval := d.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	d.runCommandTick(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runCommandTick(ctx)
		}
	}
}

func (d *Daemon) runCommandTick(ctx context.Context) {
	commands, err := d.client.ClaimDaemonCommands(ctx, d.cfg.DaemonID, 5)
	if err != nil {
		d.logger.Debug("claim daemon commands failed", "error", err)
		return
	}
	for _, command := range commands {
		if command.CommandType != "open_intellij" {
			_ = d.client.CompleteDaemonCommand(ctx, d.cfg.DaemonID, command.ID, "failed", "unsupported command type")
			continue
		}
		workDir, _ := command.Payload["work_dir"].(string)
		branchName, _ := command.Payload["branch_name"].(string)
		if err := openIntelliJ(ctx, workDir, resolveIntelliJCommand(d.cfg), branchName); err != nil {
			_ = d.client.CompleteDaemonCommand(ctx, d.cfg.DaemonID, command.ID, "failed", err.Error())
			continue
		}
		_ = d.client.CompleteDaemonCommand(ctx, d.cfg.DaemonID, command.ID, "completed", "")
	}
}
