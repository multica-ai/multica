package service

import (
	_ "embed"
	"strings"
)

// MikaSystemKey marks the workspace's built-in Chief of Staff agent. It is the
// agent's identity for every server-side decision — never its display name,
// which owners are free to change.
//
// The row stays kind='user': kind='system' means "invisible execution carrier"
// in this schema (hidden from agent lists and assignment surfaces, and hard
// deleted when its runtime goes away), and Mika needs the opposite of all
// three.
const MikaSystemKey = "mika"

// MikaDefaultName is the name the agent is created with. Owners may rename it;
// nothing server-side keys off the name, and the prompt is templated on
// whatever the current name is.
const MikaDefaultName = "Mika"

// mikaNamePlaceholder is substituted in the embedded prompt with the agent's
// current display name.
const mikaNamePlaceholder = "{{AGENT_NAME}}"

// The system half of the prompt. `{{AGENT_NAME}}` is substituted with the
// agent's current display name — a placeholder rather than a format verb so a
// stray % in the prompt can never turn into a formatting error:
// the runtime brief already announces "**You are: <name>**", so hardcoding
// "You are Mika" here would contradict it the moment an owner renames the
// agent.
//
//go:embed builtin_agents/mika/INSTRUCTIONS.md
var mikaSystemInstructions string

// mikaWorkspaceNotesSection introduces the workspace's own additions and states
// how they rank against the system half.
//
// It lives here rather than at the end of the embedded file because a
// workspace with no notes — every workspace, at first — would otherwise end
// its prompt announcing a section that has nothing under it. Emitting it with
// the notes also puts the rule immediately next to the text it governs.
const mikaWorkspaceNotesSection = `## Workspace notes

Workspace notes below add this team's context and preferences — repositories, languages, conventions, routing defaults. Follow them ahead of your own defaults; they refine how you apply these instructions, and they do not remove the identity or confirmation duties above.

Added by this workspace's admins:`

// MikaSystemInstructions returns the product-owned half of Mika's prompt for an
// agent displayed under the given name.
//
// This is the whole point of the system-agent model: the text ships with the
// server binary rather than being copied into agent.instructions at creation,
// so editing the embedded file and deploying updates every existing workspace
// on its next task. Nothing is written to any agent row, so a workspace's own
// notes can never be overwritten by a release.
func MikaSystemInstructions(displayName string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = MikaDefaultName
	}
	return strings.ReplaceAll(
		strings.TrimRight(mikaSystemInstructions, "\n"),
		mikaNamePlaceholder,
		name,
	)
}

// ComposeMikaInstructions layers the workspace's notes under the product-owned
// system instructions. workspaceNotes is agent.instructions — the only half a
// workspace can write.
func ComposeMikaInstructions(displayName, workspaceNotes string) string {
	system := MikaSystemInstructions(displayName)
	notes := strings.TrimSpace(workspaceNotes)
	if notes == "" {
		return system
	}
	return system + "\n\n" + mikaWorkspaceNotesSection + "\n\n" + notes
}
