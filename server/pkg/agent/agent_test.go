package agent

import (
	"context"
	"testing"
)

func TestNewReturnsClaudeBackend(t *testing.T) {
	t.Parallel()
	b, err := New("claude", Config{ExecutablePath: "/nonexistent/claude"})
	if err != nil {
		t.Fatalf("New(claude) error: %v", err)
	}
	if _, ok := b.(*claudeBackend); !ok {
		t.Fatalf("expected *claudeBackend, got %T", b)
	}
}

func TestNewReturnsCodexBackend(t *testing.T) {
	t.Parallel()
	b, err := New("codex", Config{ExecutablePath: "/nonexistent/codex"})
	if err != nil {
		t.Fatalf("New(codex) error: %v", err)
	}
	if _, ok := b.(*codexBackend); !ok {
		t.Fatalf("expected *codexBackend, got %T", b)
	}
}

func TestNewReturnsCopilotBackend(t *testing.T) {
	t.Parallel()
	b, err := New("copilot", Config{ExecutablePath: "/nonexistent/copilot"})
	if err != nil {
		t.Fatalf("New(copilot) error: %v", err)
	}
	if _, ok := b.(*copilotBackend); !ok {
		t.Fatalf("expected *copilotBackend, got %T", b)
	}
}

func TestNewRejectsUnknownType(t *testing.T) {
	t.Parallel()
	_, err := New("gpt", Config{})
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}

func TestNewDefaultsLogger(t *testing.T) {
	t.Parallel()
	b, _ := New("claude", Config{})
	cb := b.(*claudeBackend)
	if cb.cfg.Logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestDetectVersionFailsForMissingBinary(t *testing.T) {
	t.Parallel()
	_, err := DetectVersion(context.Background(), "/nonexistent/binary")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestLaunchHeaderCoversAllSupportedBackends(t *testing.T) {
	t.Parallel()

	// The factory in New() enumerates every supported agent type; LaunchHeader
	// must stay in sync so the UI preview never shows an empty skeleton for a
	// runtime the daemon actually spawns. If a new backend is added, add an
	// entry to launchHeaders in agent.go and extend this list.
	supported := []string{
		"claude", "codex", "copilot", "cursor", "gemini",
		"hermes", "kimi", "kiro", "openclaw", "opencode", "pi", "DeepSeek-TUI",
	}
	for _, t_ := range supported {
		if header := LaunchHeader(t_); header == "" {
			t.Errorf("LaunchHeader(%q) returned empty string — add it to launchHeaders", t_)
		}
	}
}

func TestLaunchHeaderReturnsEmptyForUnknownType(t *testing.T) {
	t.Parallel()
	if header := LaunchHeader("made-up-agent"); header != "" {
		t.Errorf("expected empty header for unknown type, got %q", header)
	}
}

func TestCapabilityRegistryCoversSupportedBackends(t *testing.T) {
	t.Parallel()

	supported := []string{
		"claude", "codebuddy", "codex", "copilot", "opencode", "openclaw", "hermes",
		"gemini", "pi", "cursor", "kimi", "kiro", "DeepSeek-TUI",
	}
	for _, provider := range supported {
		if _, ok := CapabilityFor(provider); !ok {
			t.Fatalf("CapabilityFor(%q) missing registry entry", provider)
		}
	}
}

func TestCapabilityValues(t *testing.T) {
	t.Parallel()

	claude, ok := CapabilityFor("claude")
	if !ok {
		t.Fatal("missing claude capability")
	}
	if !claude.StreamDisplay || !claude.ToolCallStream || !claude.Approval || !claude.ResumeSession || !claude.PlanMode || !claude.StructuredOutput {
		t.Fatalf("unexpected claude capability: %+v", claude)
	}

	codex, ok := CapabilityFor("codex")
	if !ok {
		t.Fatal("missing codex capability")
	}
	if !codex.StreamDisplay || !codex.ToolCallStream || !codex.Approval || !codex.ResumeSession || codex.PlanMode || !codex.StructuredOutput {
		t.Fatalf("unexpected codex capability: %+v", codex)
	}

	gemini, ok := CapabilityFor("gemini")
	if !ok {
		t.Fatal("missing gemini capability")
	}
	if !gemini.StreamDisplay || !gemini.ToolCallStream || gemini.Approval || !gemini.ResumeSession || gemini.PlanMode || !gemini.StructuredOutput {
		t.Fatalf("unexpected gemini capability: %+v", gemini)
	}

	if got := CapabilityOrDefault("unknown"); got != (Capability{}) {
		t.Fatalf("unknown capability = %+v", got)
	}
}

func TestStreamDisplayGatingForUnknownProvider(t *testing.T) {
	t.Parallel()

	cap := CapabilityOrDefault("unregistered_provider")
	if cap.StreamDisplay {
		t.Fatal("unknown provider must default to StreamDisplay=false")
	}

	for _, name := range []string{"claude", "codex", "copilot", "opencode"} {
		cap, ok := CapabilityFor(name)
		if !ok {
			t.Fatalf("missing %s", name)
		}
		if !cap.StreamDisplay {
			t.Fatalf("%s must have StreamDisplay=true", name)
		}
	}
}

func TestCapabilityRegistryNoDriftFromSupportedBackends(t *testing.T) {
	t.Parallel()

	backends := SupportedBackends()
	providers := RegisteredProviders()

	backendSet := make(map[string]bool, len(backends))
	for _, b := range backends {
		backendSet[b] = true
	}
	providerSet := make(map[string]bool, len(providers))
	for _, p := range providers {
		providerSet[p] = true
	}

	for _, b := range backends {
		if !providerSet[b] {
			t.Errorf("supported backend %q missing from capability registry", b)
		}
	}
	for _, p := range providers {
		if !backendSet[p] {
			t.Errorf("capability registry entry %q not in supported backends", p)
		}
	}
}
