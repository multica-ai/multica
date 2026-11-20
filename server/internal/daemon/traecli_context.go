package daemon

import (
	"context"
	"os/exec"
	"path/filepath"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func probeTraeLaunchVersion(ctx context.Context, executable string, prefix []string) (string, string) {
	path, err := exec.LookPath(executable)
	if err != nil || !filepath.IsAbs(path) {
		return "", ""
	}
	target, err := canonicalPath(path)
	if err != nil {
		return "", ""
	}
	version, err := detectAgentVersion(ctx, agent.NewCommand(target, prefix).ForVersionProbe("traecli"))
	if err != nil {
		return "", ""
	}
	return target, version
}

func traeRuntimeBriefPrompt(prompt, brief string, fileWritten bool) (string, string) {
	if !fileWritten {
		return prompt, brief
	}
	// Project instructions can be overridden or exceed TraeX's read limit.
	const reference = "Follow the Multica runtime brief in the task's `AGENTS.md`. " +
		"If the `MULTICA-RUNTIME` block is missing or incomplete in your context, read the block before acting."
	return reference + "\n\n" + prompt, ""
}
