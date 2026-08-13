package execenv

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillsDirPathZcode is the regression guard for the provider-native skills
// directory mapping for zcode. ZCode CLI scans .zcode/skills/ (and
// .agents/skills/) in the workdir; before the mapping was added, skillsDirPath
// fell back to .agent_context/skills/, which zcode never inspects — so bound
// skills silently never reached the agent.
func TestSkillsDirPathZcode(t *testing.T) {
	t.Parallel()
	workDir := "/work/repo"
	got := skillsDirPath(workDir, "zcode")
	want := filepath.Join(workDir, ".zcode", "skills")
	if got != want {
		t.Fatalf("skillsDirPath(zcode) = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, filepath.FromSlash("/.zcode/skills")) {
		t.Errorf("skillsDirPath(zcode) = %q, want it to end with .zcode/skills", got)
	}
	// It must NOT fall back to the generic .agent_context/skills dir.
	if strings.Contains(got, ".agent_context") {
		t.Errorf("skillsDirPath(zcode) fell back to .agent_context/skills: %q", got)
	}
}
