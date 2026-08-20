---
name: multica-repo-workspace
description: Use when registering GitHub repos in the Multica workspace (multica repo add vs multica project resource add) OR when an agent suspects GitHub integration/credentials are missing. GitHub auth is provisioned per-pod via a dynamic GitHub App installation-token flow (no static PAT/GITHUB_TOKEN); empty token env vars and 'gh auth status: not logged in' are expected and NOT signs of broken auth — verify with a real git/gh operation instead.
---

# Multica Repo Workspace Skill

**Use when registering a GitHub repository with the Multica workspace so all agents can access it.**

## Key Fact

Repositories are registered **workspace-wide** using:

```bash
multica repo add --url <github-url>
```

This makes the repo available to all agents and autopilots in the workspace. It does **not** attach the repo to any specific project.

## Adding a Repo to the Workspace

```bash
multica repo add --url https://github.com/org/repo-name
```

To verify it was registered:

```bash
multica repo list
```

## Common Confusion: Project Resource vs Workspace Registry

| Command | Scope | Effect |
|---|---|---|
| `multica repo add --url <url>` | **Workspace-wide** | Registers repo for all agents |
| `multica project resource add` | **Project-scoped** | Attaches repo to one project only |

Always use `multica repo add` when the goal is workspace-wide availability. Use `multica project resource add` only when you want to scope a repo to a specific project's context.

## Verifying Registration

```bash
# List all workspace-registered repos
multica repo list

# Check out a registered repo in your working directory
multica repo checkout https://github.com/org/repo-name

# Check out a specific branch, tag, or commit
multica repo checkout https://github.com/org/repo-name --ref feature-branch
```

## Tips

- `multica repo checkout` creates a git worktree with a dedicated branch for your agent session.
- Run `multica repo --help` to see all repo subcommands.
- You do not need to register a repo before checking it out; `multica repo checkout` works for any accessible GitHub URL.

## Common Confusion: "GitHub credentials are missing" (they aren't)

GitHub access here is provisioned **per-pod via a dynamic GitHub App
installation-token flow** — never a static PAT/`GITHUB_TOKEN`/`GH_TOKEN` env
var. `git`'s `credential.helper` mints a short-lived installation token per
repo at the moment git needs it; `gh` is separately wrapped to inject a fresh
token per invocation the same way. Neither leaves a token sitting in env or
in `gh auth status`.

Do **not** conclude "GitHub credentials are missing" from:

- An empty `GITHUB_TOKEN`/`GH_TOKEN` env var — intentionally never set statically.
- `gh auth status` reporting "not logged in" — that command has no repo
  context to resolve an installation from, so it legitimately falls through
  to the real, unauthenticated `gh`. It is **not** a signal of actual auth failure.

To check auth is actually usable:

- Run the real operation against the target repo (`git ls-remote <url>`,
  `git push`, `gh pr create` from inside the checkout) and read the actual
  error, or
- Check `git config --get credential.https://github.com.helper` is set to
  `/usr/local/bin/git-credential-platform-bot`.
