package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInjectRuntimeConfigHeaderIsRoleNeutral guards the fix for #1216.
// The platform-injected header must not claim "coding agent" — Multica hosts
// CEO/analyst/marketing/etc. agents whose configured identity would be
// contradicted by that framing. Coding specifics belong in skills, workflow
// sections, or the agent's own instructions.
func TestInjectRuntimeConfigHeaderIsRoleNeutral(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID:   "11111111-1111-1111-1111-111111111111",
		AgentName: "Victor",
	}
	if err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	content := string(b)

	// Header must not prejudge the agent's role.
	if strings.Contains(content, "You are a coding agent") {
		t.Errorf("injected header should not claim 'coding agent' — breaks non-coding roles (#1216)\n---\n%s", content)
	}

	// But it must still identify the environment so agents know how to interact.
	for _, want := range []string{
		"Multica platform",
		"`multica` CLI",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("header dropped required phrase %q\n---\n%s", want, content)
		}
	}
}
