---
name: release-desktop
description: Build, sign, notarize, and publish Multica Desktop releases for macOS arm64 and x64 to an existing GitHub Release. Use after the matching version tag and CLI release exist; do not use for Windows or Linux releases.
---

# Release Multica Desktop for macOS

Publish the signed and notarized macOS arm64 and x64 Desktop artifacts to the
GitHub Release that the tag-based CLI release flow already created. Run the
packaging command locally on the designated Mac so Apple credentials stay on
that host.

Packaging, signing, notarization, and uploading can be quiet for many minutes.
Keep the build in the foreground and wait for it to exit; do not treat quiet
output as a hang.

## Required input

Extract a tag such as `v0.18.4` from the user's request. Require the leading
`v`; if no tag was provided, ask for it instead of guessing.

## Preflight

Resolve the Multica checkout from `MULTICA_REPO`, falling back to the current
directory only when it contains `apps/desktop/electron-builder.yml`:

```bash
REPO="${MULTICA_REPO:-$PWD}"
[ -f "$REPO/apps/desktop/electron-builder.yml" ] || {
  echo "not a multica repo: $REPO — set MULTICA_REPO or cd into the repo"
  exit 1
}
cd "$REPO"
```

Verify all prerequisites before changing refs or starting a build:

```bash
gh auth status --active --hostname github.com
git status --porcelain
git fetch --tags origin
git rev-parse "$TAG^{commit}"
gh release view "$TAG" --json tagName,assets
command -v go
command -v node
command -v pnpm
```

`gh auth status` must be scoped to the active `github.com` account. An
unfiltered status checks every stored account and exits 1 if an unused account
has stale credentials, even when the active release account is valid. Do not
pipe this check through `head`; its human-readable status is written to stderr,
and a pipeline can hide which command actually failed. Confirm the active
account has `repo` scope in the displayed status.

Require `git status --porcelain` to be empty. Never auto-stash user work.

Resolve the credentials file from `MULTICA_RELEASE_ENV`, defaulting to
`$HOME/.multica/release.env`. It must be owner-only and export `APPLE_ID`,
`APPLE_APP_SPECIFIC_PASSWORD`, and `APPLE_TEAM_ID`; it may also export
`APPLE_SIGNING_IDENTITY`. Do not search elsewhere for credentials when it is
missing.

```bash
RELEASE_ENV="${MULTICA_RELEASE_ENV:-$HOME/.multica/release.env}"
[ -f "$RELEASE_ENV" ] || {
  echo "missing $RELEASE_ENV"
  exit 1
}
```

Use `security find-identity -v -p codesigning` to confirm a Developer ID
Application identity exists and its team ID matches `APPLE_TEAM_ID`. If it is
missing or mismatched, stop; do not install or switch credentials.

Inspect the existing release assets. If the target release already has any
macOS Desktop asset for this version, `latest-mac.yml`, or
`latest-x64-mac.yml`, ask the user whether to replace them. After explicit
confirmation, delete only those stale macOS assets with
`gh release delete-asset "$TAG" <asset-name> --yes`. Never delete CLI, Windows,
or Linux assets.

## Checkout and environment

Record the original branch or commit before checking out the tag. Always return
to it when the release attempt ends, including after failures.

```bash
ORIGINAL_REF=$(git symbolic-ref --short HEAD 2>/dev/null || git rev-parse HEAD)
git checkout "$TAG"
```

Load the Apple credentials and surface GitHub CLI's active token for
electron-builder, which does not read the `gh` keychain directly:

```bash
set -a
source "$RELEASE_ENV"
set +a
[ -n "$APPLE_ID" ] &&
  [ -n "$APPLE_APP_SPECIFIC_PASSWORD" ] &&
  [ -n "$APPLE_TEAM_ID" ]

export GH_TOKEN=$(gh auth token --hostname github.com)
[ -n "$GH_TOKEN" ] || {
  echo "gh auth token returned empty — run 'gh auth login' first"
  exit 1
}
```

Never print the credential file or token.

## Build and publish

Run both commands in the foreground:

```bash
pnpm install --frozen-lockfile
pnpm --filter @multica/desktop package -- --mac --arm64 --x64 --publish always
```

The package wrapper derives the version from the checked-out tag, bundles the
matching CLI, signs and notarizes both architectures, staples the apps before
creating the update archives, and publishes the generated assets. Do not
re-staple or hand-upload individual files: doing so can invalidate the SHA512
values in electron-updater metadata.

If packaging fails, report the failing architecture and the last relevant
output. A partial run may already have uploaded artifacts, so a retry must
repeat the asset inspection and confirmation in preflight.

## Verify

List and sort the release assets:

```bash
gh release view "$TAG" --json assets --jq '.assets[].name' | sort
```

Require the complete ten-file macOS set, where `<version>` is the tag without
the leading `v`:

- `multica-desktop-<version>-mac-arm64.dmg`
- `multica-desktop-<version>-mac-arm64.dmg.blockmap`
- `multica-desktop-<version>-mac-arm64.zip`
- `multica-desktop-<version>-mac-arm64.zip.blockmap`
- `latest-mac.yml`
- `multica-desktop-<version>-mac-x64.dmg`
- `multica-desktop-<version>-mac-x64.dmg.blockmap`
- `multica-desktop-<version>-mac-x64.zip`
- `multica-desktop-<version>-mac-x64.zip.blockmap`
- `latest-x64-mac.yml`

If any file is missing, stop and report the incomplete set.

## Cleanup and report

Restore the original ref even after a failure:

```bash
git checkout "$ORIGINAL_REF"
```

Report the tag and commit, derived Desktop version, verified macOS asset list,
and the URL from:

```bash
gh release view "$TAG" --json url --jq .url
```

Mention that arm64 clients use `latest-mac.yml` and Intel clients use
`latest-x64-mac.yml`.

## Safety boundaries

- Never create tags or GitHub Releases, force-push, or touch the user's main
  branch.
- Never switch GitHub accounts or run interactive `gh auth` flows without the
  user's approval.
- Never upload credential files, certificates, or manually assembled updater
  artifacts.
- Never delete non-macOS release assets.
- Never commit build-time version changes or generated release output.
- Never terminate the packaging command merely because it is slow or quiet.
- Windows and Linux Desktop assets remain owned by the GitHub Actions release
  workflow.
