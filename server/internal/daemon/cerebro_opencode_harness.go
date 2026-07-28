package daemon

import (
	"fmt"

	opencodeharness "github.com/multica-ai/multica/packages/cerebro-opencode-harness"
)

// prepareOpenCodeHarness installs OpenCode's tool-policy adapter: a Firtal
// plugin whose `tool.execute.before` hook resolves every tool call against the
// daemon's loopback policy IPC and throws to abort a denied call.
//
// The plugin IS the gate, so a failure to install it must stop the spawn.
// Starting OpenCode without it would run the agent with no call-time
// enforcement at all — the exact hole the mandatory-adapter rule exists to
// close. Other providers are untouched.
func prepareOpenCodeHarness(provider, workdir string) error {
	if provider != "opencode" {
		return nil
	}
	if _, err := opencodeharness.Prepare(workdir); err != nil {
		return fmt.Errorf("install OpenCode tool-policy plugin: %w", err)
	}
	if !opencodeharness.Installed(workdir) {
		return fmt.Errorf("OpenCode tool-policy plugin is not installed in %q; refusing to start an unenforced run", workdir)
	}
	return nil
}
