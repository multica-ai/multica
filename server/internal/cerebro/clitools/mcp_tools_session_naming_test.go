package clitools

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/mcp"
)

func TestRenameSessionToolRequiresPurposeSpecificWorkflowNames(t *testing.T) {
	t.Parallel()

	srv := mcp.NewServer("test", "0")
	RegisterTools(srv, cli.NewAPIClient("", "", ""), &SessionState{}, "workspace-1", "", "")

	var description string
	for _, tool := range srv.Tools() {
		if tool.Name == "rename_session" {
			description = tool.Description
			break
		}
	}
	if description == "" {
		t.Fatal("rename_session tool was not registered")
	}

	for _, generic := range []string{
		`name the plan session "Plan"`,
		`"Build 1"/"Review 1"`,
	} {
		if strings.Contains(description, generic) {
			t.Errorf("rename_session still recommends generic workflow names %q: %s", generic, description)
		}
	}

	for _, want := range []string{
		"human-readable",
		"concrete purpose",
		"phase badge",
		"Plan descriptive thread naming",
		"Build contextual naming guidance",
	} {
		if !strings.Contains(description, want) {
			t.Errorf("rename_session description missing %q: %s", want, description)
		}
	}
}
