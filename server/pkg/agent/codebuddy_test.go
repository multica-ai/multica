package agent

import (
	"testing"
)

func TestNewReturnsCodebuddyBackend(t *testing.T) {
	b, err := New("codebuddy", Config{})
	if err != nil {
		t.Fatalf("New(codebuddy) error: %v", err)
	}
	cb, ok := b.(*codebuddyBackend)
	if !ok {
		t.Fatalf("expected *codebuddyBackend, got %T", b)
	}
	if cb.inner == nil {
		t.Fatal("codebuddyBackend.inner is nil")
	}
	if cb.inner.cfg.ExecutablePath != "cbc" {
		t.Errorf("expected default exec path 'cbc', got %q", cb.inner.cfg.ExecutablePath)
	}
}

func TestNewCodebuddyHonoursExplicitPath(t *testing.T) {
	b, err := New("codebuddy", Config{ExecutablePath: "/opt/cbc"})
	if err != nil {
		t.Fatalf("New(codebuddy) error: %v", err)
	}
	cb := b.(*codebuddyBackend)
	if cb.inner.cfg.ExecutablePath != "/opt/cbc" {
		t.Errorf("expected explicit exec path '/opt/cbc', got %q", cb.inner.cfg.ExecutablePath)
	}
}

func TestCodebuddyCapabilitiesMatchClaude(t *testing.T) {
	claude := Capabilities("claude")
	cb := Capabilities("codebuddy")
	if claude != cb {
		t.Errorf("codebuddy capabilities %+v differ from claude %+v", cb, claude)
	}
}

func TestCodebuddyLaunchHeader(t *testing.T) {
	if got := LaunchHeader("codebuddy"); got != "codebuddy (stream-json)" {
		t.Errorf("unexpected launch header: %q", got)
	}
}

func TestCodebuddyStaticModelsHasDefault(t *testing.T) {
	models := codebuddyStaticModels()
	if len(models) == 0 {
		t.Fatal("codebuddyStaticModels returned empty list")
	}
	var defaults int
	for _, m := range models {
		if m.Default {
			defaults++
		}
	}
	if defaults != 1 {
		t.Errorf("expected exactly one default model, got %d", defaults)
	}
}
