package agent

import (
	"log/slog"
	"slices"
	"testing"
)

func TestBuildInteractiveCodexLaunchOwnsTransportWorkdirAndPolicy(t *testing.T) {
	launch, err := BuildInteractiveLaunch("codex", InteractiveOptions{
		Prompt: "do work", Model: "gpt-test", ThinkingLevel: "high",
		CustomArgs: []string{"--cd", "/tmp/escape", "--sandbox=danger-full-access", "--ask-for-approval", "always", "--no-alt-screen", "--config", "feature=true"},
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/tmp/escape", "danger-full-access", "always", "--no-alt-screen"} {
		if slices.Contains(launch.Args, forbidden) {
			t.Fatalf("argv contains forbidden value %q: %v", forbidden, launch.Args)
		}
	}
	wantTail := []string{"--ask-for-approval", "never", "--sandbox", "workspace-write", "do work"}
	if !slices.Equal(launch.Args[len(launch.Args)-len(wantTail):], wantTail) {
		t.Fatalf("argv tail = %v", launch.Args)
	}
}

func TestBuildInteractiveCodexResume(t *testing.T) {
	launch, err := BuildInteractiveLaunch("codex", InteractiveOptions{Prompt: "continue", ResumeSessionID: "019fe469-33bc-75c2-9492-ca640a1788a4"}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if len(launch.Args) < 2 || launch.Args[0] != "resume" || launch.Args[1] != "019fe469-33bc-75c2-9492-ca640a1788a4" {
		t.Fatalf("resume argv = %v", launch.Args)
	}
}

func TestBuildInteractiveLaunchRejectsOtherProviderAndSubcommand(t *testing.T) {
	if _, err := BuildInteractiveLaunch("claude", InteractiveOptions{Prompt: "x"}, slog.Default()); err == nil {
		t.Fatal("expected unsupported provider")
	}
	if _, err := BuildInteractiveLaunch("codex", InteractiveOptions{Prompt: "x", CustomArgs: []string{"exec"}}, slog.Default()); err == nil {
		t.Fatal("expected nested subcommand rejection")
	}
}
