---
name: git-worktrees
description: This skill should be used when the user asks to "work on parallel features", "use worktrees", "start a new feature while working on another", "work on two branches simultaneously", "use parallel branches", "simultaneous feature development", "set up a worktree", or "create a git worktree". Only triggers when the user explicitly requests worktree-based parallel development — not by default for normal feature work.
---

# Git Worktrees for Parallel Feature Development

Git worktrees let a single repository have multiple working directories checked out simultaneously. Each worktree has its own working directory and index, but shares the same git history, remote refs, and hooks. Use this when the user explicitly asks to work on two or more features at the same time without stashing or switching branches.

## When to Use Worktrees

Use worktrees only when the user explicitly requests parallel development. Common triggers:

- "I want to work on feature A and feature B at the same time"
- "Use worktrees" or "set up a worktree for this branch"
- "Start a hotfix while I keep working on this feature"
- "Parallel branches" or "simultaneous feature development"

Do NOT create worktrees by default for normal single-feature work. The standard branch-and-switch workflow is simpler and sufficient for most cases.

## Worktree Location Convention

Always place worktrees inside the repository at `.worktrees/<branch-name>/`. This keeps them co-located with the project, makes paths predictable, and avoids polluting the filesystem with scattered directories.

Before creating any worktree, ensure `.worktrees/` is in `.gitignore`. Check first:

```bash
grep -q "^\.worktrees/" .gitignore 2>/dev/null || echo ".worktrees/" >> .gitignore
```

## Setup Workflow

### 1. Ensure the Main Tree Is Clean

Before creating a worktree, verify the main working tree has no uncommitted changes that could cause confusion. Either commit pending work or stash it:

```bash
git status --short
# If dirty:
git stash push -m "WIP before worktree setup"
# or:
git add -A && git commit -m "WIP: checkpoint before parallel work"
```

### 2. Create the Worktree

For a new branch:

```bash
git worktree add .worktrees/<branch-name> -b <branch-name>
```

For an existing branch:

```bash
git worktree add .worktrees/<branch-name> <branch-name>
```

To base the new branch on a specific commit or remote branch:

```bash
git worktree add .worktrees/<branch-name> -b <branch-name> origin/main
```

### 3. Detect Package Manager and Install Dependencies

Each worktree has its own working directory. Dependencies installed in the main tree are NOT available in the new worktree. Detect the package manager from lock files and install:

| Lock file | Command |
|-----------|---------|
| `package-lock.json` | `npm install` |
| `bun.lock` or `bun.lockb` | `bun install` |
| `yarn.lock` | `yarn install` |
| `pnpm-lock.yaml` | `pnpm install` |
| `requirements.txt` | `pip install -r requirements.txt` |
| `Pipfile` | `pipenv install` |
| `poetry.lock` | `poetry install` |
| `go.mod` | `go mod download` |
| `Cargo.toml` | `cargo fetch` |

Run the install command with `workdir` pointing to the new worktree path. Never `cd` into the worktree.

### 4. Use the Bundled Setup Script

The bundled script at `scripts/worktree-setup.sh` automates steps 1-3. Run it with the branch name as the only argument:

```bash
bash scripts/worktree-setup.sh <branch-name>
```

The script outputs the absolute path to the new worktree on success.

## Working Inside Worktrees

This is the most critical part. All subsequent commands for the feature MUST use the `workdir` parameter pointing to `.worktrees/<branch-name>/`. Never use `cd` to enter the worktree.

### Bash Commands

```bash
# Correct — use workdir parameter
bash_command("npm run build", workdir=".worktrees/feature-auth/")
bash_command("npm test", workdir=".worktrees/feature-auth/")

# Wrong — never do this
bash_command("cd .worktrees/feature-auth && npm run build")
```

### File Operations

Use absolute paths when reading or writing files inside a worktree:

```
/path/to/repo/.worktrees/feature-auth/src/components/Login.tsx
```

Relative paths resolve against the main working directory, not the worktree.

### Task Delegation

When delegating work to subagents via `task()`, pass the worktree path as `workdir`:

```python
task(
    description="Implement the auth feature",
    workdir="/path/to/repo/.worktrees/feature-auth/",
    ...
)
```

The subagent will operate entirely within the worktree, with no risk of touching the main tree.

### What Is and Isn't Shared

**Shared across all worktrees:**
- Git history and objects (`.git/objects/`)
- Remote refs and fetch state
- Git hooks (`.git/hooks/`)
- Stash entries
- Tags and branches (the refs themselves)

**NOT shared — each worktree has its own:**
- Working directory (the actual files)
- Index / staging area
- HEAD (each worktree tracks a different branch)
- `node_modules/`, `venv/`, build artifacts
- Environment variables and shell state

## Cleanup Workflow

After a feature is merged or a PR is created, remove the worktree to keep the repository tidy.

### Standard Removal

```bash
git worktree remove .worktrees/<branch-name>
```

This fails if the worktree has uncommitted changes. Either commit or discard them first, or use `--force`.

### Force Removal

```bash
git worktree remove --force .worktrees/<branch-name>
```

Use `--force` only when the work is intentionally discarded or already committed elsewhere.

### Prune Stale Entries

If a worktree directory was deleted manually (not via `git worktree remove`), git retains a stale reference. Clean it up:

```bash
git worktree prune
```

### List Active Worktrees

```bash
git worktree list
```

This shows all worktrees, their paths, HEAD commits, and branch names.

### Bundled Cleanup Script

The bundled script at `scripts/worktree-cleanup.sh` handles removal and pruning. Pass the branch name, or `--all` to remove every worktree under `.worktrees/`:

```bash
bash scripts/worktree-cleanup.sh <branch-name>
bash scripts/worktree-cleanup.sh --all
```


## Full Lifecycle: Create → Land → Delete

### Create

```bash
# 1. Ensure .worktrees/ is gitignored
grep -q "^\.worktrees/" .gitignore 2>/dev/null || echo ".worktrees/" >> .gitignore

# 2. Create worktree off main (or current branch)
git worktree add .worktrees/my-feature -b my-feature origin/main

# 3. Install deps in the worktree
# (use workdir parameter — never cd)
bundle install  # workdir=.worktrees/my-feature/
yarn install     # workdir=.worktrees/my-feature/
```

Do all work inside `.worktrees/my-feature/` using `workdir`. The main checkout stays untouched on whatever branch it was on.

### Land into Main

Worktree branches land via the normal PR flow. The worktree is just a convenient way to develop — the branch itself is a regular git branch:

```bash
# 1. Push the feature branch (from main checkout or worktree — doesn't matter)
git push -u origin my-feature

# 2. Open PR
gh pr create --head my-feature --base main --title "My feature" --body "..."

# 3. After PR merges, clean up (see Delete below)
```

Do NOT merge worktree branches directly into main locally. Always go through PR review.

### Delete (Full Cleanup)

After the PR merges:

```bash
# 1. Remove the worktree
git worktree remove .worktrees/my-feature

# 2. Delete the local feature branch
git branch -D my-feature

# 3. Prune any stale worktree refs
git worktree prune
```

## Git Lock Safety

In a remote agent environment, git lock files are the most common source of stuck worktrees. Crashes, timeouts, or concurrent subagent operations can leave orphaned locks that block all subsequent git commands.

### Verify State Before Any Operation

Always check worktree state before creating or removing:

```bash
git worktree list
```

If a worktree shows `(locked)` or `prunable`, resolve it before proceeding.

### Index Lock Files

If a git process crashes mid-operation, it may leave `.git/index.lock` (main tree) or `.git/worktrees/<name>/index.lock` (worktree). Symptoms:

```
fatal: Unable to create '/path/to/repo/.git/index.lock': File exists.
```

Diagnosis and recovery:

```bash
# Check for orphaned lock files
find .git -name "index.lock" -type f

# Verify no git process is actually running
ps aux | grep git

# If no git process is running, safe to remove
rm .git/index.lock
rm .git/worktrees/<branch-name>/index.lock
```

### Worktree Lock Files

Git creates `.git/worktrees/<name>/locked` to prevent pruning of worktrees on removable media. In a container environment these can appear spuriously.

```bash
# Check if a worktree is locked
git worktree list  # locked worktrees show "(locked)"

# Unlock a worktree
git worktree unlock .worktrees/<branch-name>

# Or manually remove the lock file
rm .git/worktrees/<branch-name>/locked
```

### Concurrent Operations — The Critical Rule

**Never run concurrent git operations against the same worktree from multiple subagents.** Git uses file-level locking that is not safe for parallel access to the same working directory.

Safe:
- Two subagents operating on **different** worktrees simultaneously
- One subagent running git commands while another reads files (no git) in a different worktree

Unsafe:
- Two subagents both running `git add` / `git commit` in the same worktree
- One subagent running `git rebase` while another runs `git status` in the same worktree

### Recovery: Stuck Worktree State

If a worktree gets into a bad state (e.g., interrupted rebase, corrupt index):

```bash
# 1. Prune any stale worktree references
git worktree prune

# 2. If worktree directory exists but git doesn't recognize it
git worktree repair

# 3. Nuclear option: force-remove and recreate
git worktree remove --force .worktrees/<branch-name>
git worktree add .worktrees/<branch-name> -b <branch-name> origin/main
```

## Common Pitfalls

### Same Branch in Two Worktrees

Git prevents checking out the same branch in two worktrees simultaneously. Attempting it produces:

```
fatal: '<branch>' is already checked out at '<path>'
```

Each worktree must track a distinct branch. To work on the same logical feature from two angles, create a second branch off the first.

### Uncommitted Changes Block Removal

`git worktree remove` refuses to delete a worktree with uncommitted changes. Either commit, stash, or use `--force`.

### Missing .gitignore Entry

Without `.worktrees/` in `.gitignore`, git will show the worktree directories as untracked files in the main tree. Always add the entry before creating the first worktree.

### Dependencies Are Per-Worktree

`node_modules/`, Python virtual environments, compiled artifacts — none of these are shared. Every new worktree needs a fresh install. The setup script handles this automatically.

### Submodules Need Manual Init

If the repository uses git submodules, run this inside the new worktree after creation:

```bash
git submodule update --init --recursive
```

The setup script does not handle submodules automatically.

## Quick Reference

```bash
# Create worktree for new branch
git worktree add .worktrees/feature-x -b feature-x

# Create worktree for existing branch
git worktree add .worktrees/feature-x feature-x

# List all worktrees
git worktree list

# Remove worktree
git worktree remove .worktrees/feature-x

# Force remove (uncommitted changes)
git worktree remove --force .worktrees/feature-x

# Prune stale references
git worktree prune
```

## Scripts

- **`scripts/worktree-setup.sh`** — Creates a worktree, adds `.gitignore` entry, detects package manager, installs dependencies. Takes branch name as argument.
- **`scripts/worktree-cleanup.sh`** — Removes a worktree and prunes stale entries. Takes branch name or `--all`.
