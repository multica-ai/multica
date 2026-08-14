# Multica Skills

## multica-operator

`multica-operator` is a portable Agent Skill for connecting to Multica,
authenticating an account, and inspecting workspaces from an AI coding agent.
Its frontmatter follows the open Agent Skills format and contains no
host-specific fields.

Marketplace installation is preferred because the host owns updates and
uninstallation.

### Codex

```bash
codex plugin marketplace add multica-ai/multica --ref marketplace
codex plugin add multica-operator@multica
```

To update:

```bash
codex plugin marketplace upgrade multica
codex plugin add multica-operator@multica
```

Start a new Codex thread after installation or update.

### Claude Code

```text
/plugin marketplace add https://github.com/multica-ai/multica.git#marketplace
/plugin install multica-operator@multica
```

To update:

```bash
claude plugin marketplace update multica
claude plugin update multica-operator@multica
```

Follow Claude Code's reload prompt or start a new session after installation or
update.

### Cursor

Install Multica Operator from Cursor Marketplace after the current version has
passed Cursor's review. Cursor review can lag behind the GitHub release.
Update it through Cursor Marketplace after the new version passes review, then
start a new agent session.

### Other Agent Skills hosts

Use the host's native Git installer with repository `multica-ai/multica`, ref
`marketplace`, and Skill path
`plugins/multica-operator/skills/multica-operator`. Do not use `main` or a
`vX.Y.Z` source tag with this generated path; the generated plugin exists only
on the `marketplace` branch.

For an immutable version, or for hosts that only support a local Skill
directory, install the complete versioned Skill archive from the matching
GitHub Release into the host's Skill location:

| Agent host | Install directory |
| --- | --- |
| Codex | `$CODEX_HOME/skills/multica-operator/` |
| Claude Code | `.claude/skills/multica-operator/` |
| Other Agent Skills hosts | Host-specific Skill directory |

After installation or upgrade, restart the host or begin a new session if it
does not refresh Skills automatically. The Skill never updates its own files.

The skill uses the Multica CLI. It checks for the CLI before connecting and asks
for approval before installing it. CLI installers are documented in
[`apps/docs/content/docs/cli.mdx`](../apps/docs/content/docs/cli.mdx), including
macOS, Linux, Homebrew, and Windows PowerShell.
