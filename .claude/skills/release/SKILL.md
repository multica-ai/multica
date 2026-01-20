---
name: release
description: Automate the Multica application release workflow including version bump, code checks, build, and GitHub release.
---

# Release Skill

Automates the complete release workflow for Multica application.

## How to use

- `/release`
  Start the release workflow interactively.

## Release Workflow

Execute the following steps in order. Stop immediately if any step fails.

### Step 1: Git Branch Check

1. Verify current branch is `main`
2. Pull latest changes from remote
3. Ensure working directory is clean (no uncommitted changes)

```bash
# Check current branch is main
current_branch=$(git branch --show-current)
if [ "$current_branch" != "main" ]; then
  echo "Error: Must be on main branch. Current branch: $current_branch"
  exit 1
fi

# Pull latest changes
git pull origin main

# Check for uncommitted changes
if [ -n "$(git status --porcelain)" ]; then
  echo "Error: Working directory has uncommitted changes"
  exit 1
fi
```

If not on main branch, abort and ask user to switch to main first.

### Step 2: Environment Check

1. Check if `.env` file exists in the project root
2. Verify the following Apple credentials are set in `.env`:
   - `APPLE_ID`
   - `APPLE_APP_SPECIFIC_PASSWORD`
   - `APPLE_TEAM_ID`
3. Check GitHub CLI authentication: `gh auth status`
4. If any check fails, inform the user what's missing and abort

```bash
# Check .env exists
test -f .env

# Check required variables (source and verify)
source .env
test -n "$APPLE_ID" && test -n "$APPLE_APP_SPECIFIC_PASSWORD" && test -n "$APPLE_TEAM_ID"

# Check gh CLI
gh auth status
```

### Step 3: Code Quality Checks

Run all quality checks before making any changes. Abort if any fail:

```bash
pnpm typecheck      # TypeScript compilation check
pnpm lint           # ESLint check
pnpm format:check   # Prettier format check
pnpm test:run       # Unit tests
```

### Step 4: Version Confirmation

1. Read current version from `package.json`
2. Ask the user for the new version number (e.g., `0.1.4`)
3. Update the `version` field in `package.json`

Use AskUserQuestion tool with options like:

- Patch bump (current → next patch)
- Minor bump (current → next minor)
- Major bump (current → next major)
- Custom version

### Step 5: Build Application

1. Clean previous build artifacts
2. Source the environment variables
3. Run the Mac build which creates both architectures

```bash
# Clean previous build
rm -rf dist/

# Build
source .env && pnpm build:mac
```

This will:

- Build arm64 (Apple Silicon) version
- Build x64 (Intel) version
- Automatically notarize with Apple (notarize: true in config)
- Output files to `dist/` directory:
  - `Multica-{version}-arm64.dmg` and `.blockmap`
  - `Multica-{version}-x64.dmg` and `.blockmap`
  - `Multica-{version}-arm64-mac.zip` and `.blockmap`
  - `Multica-{version}-x64-mac.zip` and `.blockmap`
  - `latest-mac.yml` (auto-updater manifest)

### Step 6: Commit Version Change

Commit the version bump and push to remote:

```bash
git add package.json
git commit -m "chore: bump version to {version}"
git push origin main
```

### Step 7: Generate Release Notes

1. Get the latest release tag:

   ```bash
   gh release list --limit 1 --json tagName -q '.[0].tagName'
   ```

2. Get commits since last release (or all commits if first release):

   ```bash
   # If there's a previous release tag:
   git log {last_tag}..HEAD --oneline

   # If this is the first release (no previous tag):
   git log --oneline
   ```

3. Analyze the commits and generate release notes with sections:
   - **New Features** - New functionality added
   - **Bug Fixes** - Issues resolved
   - **Improvements** - Enhancements to existing features
   - **Other Changes** - Documentation, refactoring, etc.

Format example:

```markdown
## What's New

### New Features

- Feature description from commit

### Bug Fixes

- Fix description from commit

### Improvements

- Improvement description from commit
```

### Step 8: Create GitHub Release

1. Create the release with generated notes and all build artifacts:

   ```bash
   gh release create v{version} \
     --title "v{version}" \
     --notes "{release_notes}" \
     dist/Multica-{version}-arm64.dmg \
     dist/Multica-{version}-arm64.dmg.blockmap \
     dist/Multica-{version}-x64.dmg \
     dist/Multica-{version}-x64.dmg.blockmap \
     dist/Multica-{version}-arm64-mac.zip \
     dist/Multica-{version}-arm64-mac.zip.blockmap \
     dist/Multica-{version}-x64-mac.zip \
     dist/Multica-{version}-x64-mac.zip.blockmap \
     dist/latest-mac.yml
   ```

   The uploaded files include:
   - **DMG files**: Primary installers for each architecture
   - **ZIP files**: Alternative distribution format (required by auto-updater)
   - **Blockmap files**: Enable incremental/delta updates
   - **latest-mac.yml**: Manifest file for electron-updater to check for new versions

2. Confirm the release was created successfully
3. Provide the release URL to the user

## Important Notes

- Never skip any step
- Always wait for build completion before proceeding
- The build process may take several minutes due to notarization
- If build fails, check Apple credential validity
