# Fork runbook (`Git-on-my-level/multica`)

This file is **fork-only**. Upstream does not have it. Keep it when merging
`upstream/main` — do not delete it to resolve conflicts.

It captures how this fork differs from `multica-ai/multica` so we do not re-learn
install, release, desktop signing, or self-host env every cycle.

## Quick map

| Concern | Source of truth |
|---------|-----------------|
| Install CLI (humans / UI copy) | `scripts/install-fork.sh` → `install.sh` |
| UI install command | Server env `MULTICA_GITHUB_REPO` → `/api/config` → `buildCliInstallCommand()` |
| CLI binaries on tag | `.goreleaser.fork.yml` via unified `.github/workflows/release.yml` |
| Docker images on tag | Same `release.yml` docker jobs (`ghcr.io/<lowercase-owner>/…`) |
| macOS desktop | Manual or CI arm64; **must** be Developer ID + notarized — see [`apps/desktop/MACOS_RELEASE.md`](apps/desktop/MACOS_RELEASE.md) |
| Self-host (non-Docker) | Build from git tag; set fork env vars below |

## Fork-only files (safe through upstream merges)

These should never be expected upstream. Prefer putting fork defaults here:

| Path | Role |
|------|------|
| `FORK.md` | This runbook |
| `scripts/install-fork.sh` | curl entrypoint; sets `MULTICA_GITHUB_REPO`, skips Homebrew |
| `scripts/install-fork.ps1` | Windows parity |
| `.goreleaser.fork.yml` | GoReleaser without Homebrew tap publish |
| `.github/workflows/release.yml` | Single tag pipeline: CLI + Docker + desktop (macOS fork-only job) |
| `apps/desktop/MACOS_RELEASE.md` | Signing / notarization / Gatekeeper |
| `scripts/cleanup-macos-desktop-updater.sh` | Clear ShipIt backups + updater cache |

Upstreamable changes (env-guarded) live in shared files: `scripts/install.sh`,
`packages/core/constants/github.ts`, `server/internal/cli/update.go`, etc.

## Install CLI

**Canonical command** (also what Connect Remote should show when the server has
fork env set):

```bash
MULTICA_GITHUB_REPO=Git-on-my-level/multica MULTICA_GITHUB_BRANCH=main \
  curl -fsSL https://raw.githubusercontent.com/Git-on-my-level/multica/main/scripts/install-fork.sh | bash
```

Behavior:

1. Skips Homebrew (`multica-ai/tap` would install upstream).
2. Downloads CLI from **this fork’s** GitHub Releases when a tag exists.
3. Falls back to `go build` from `MULTICA_CLI_REF` (default `main`).
4. On Apple Silicon, prefers `/opt/homebrew/bin` when present (not `/usr/local/bin`).
5. Persists `github_repo` in `~/.multica/config.json` so `multica update` stays on the fork.

**Do not** curl `install.sh` alone on a brew machine — it may still prefer
upstream Homebrew unless `MULTICA_SKIP_BREW=1` / non-upstream repo is set.

## Self-host / backend env (required for UI)

Backend must expose the fork on `/api/config` or the UI keeps showing upstream
install URLs:

```bash
MULTICA_GITHUB_REPO=Git-on-my-level/multica
MULTICA_GITHUB_BRANCH=main
```

Verify:

```bash
curl -fsS "$PUBLIC_URL/api/config" | jq '{github_repo, github_branch}'
# → "Git-on-my-level/multica", "main"
```

Non-Docker deploy: checkout a release tag (e.g. `v0.3.45`), migrate, `make build`,
rebuild web, restart services. GHCR images are optional for this fork’s primary
host (source build is fine).

Docker self-host (if used):

```bash
MULTICA_BACKEND_IMAGE=ghcr.io/git-on-my-level/multica-backend
MULTICA_WEB_IMAGE=ghcr.io/git-on-my-level/multica-web
MULTICA_IMAGE_TAG=v0.3.45
```

GHCR owner must be **lowercase** (`git-on-my-level`).

## Release checklist

1. Merge upstream `main` into fork `main`. Resolve conflicts only in fork-only
   files. Desktop publish owner/repo is injected by CI (`--config.publish.*`); do not bake the fork org into `electron-builder.yml`.
2. Local check: `pnpm typecheck` / `make test` as needed.
3. Tag and push: `git tag vX.Y.Z && git push origin vX.Y.Z`
4. Wait for CI (`release.yml` only — `release-fork.yml` was merged into it):
   - CLI archives + `checksums.txt` via `.goreleaser.fork.yml`
   - GHCR backend/web (`ghcr.io/git-on-my-level/…`)
   - Desktop Linux/Win; macOS when Apple secrets allow signing
5. **macOS desktop** (until CI secrets are complete): build/sign/notarize locally
   per [`apps/desktop/MACOS_RELEASE.md`](apps/desktop/MACOS_RELEASE.md) and publish
   to the same tag (`latest-mac.yml` + DMG/ZIP).
6. Point self-host at the new tag; smoke-test install-fork on a Mac runtime.

### Lessons already paid for

| Symptom | Cause | Fix |
|---------|-------|-----|
| Install lands upstream CLI | Homebrew path in `install.sh` | Use `install-fork.sh` / `MULTICA_SKIP_BREW=1` |
| Install “succeeds” but binary missing | `/usr/local/bin` missing on Apple Silicon | Prefer `/opt/homebrew/bin`; `mkdir -p`; fail on `mv` error |
| UI shows upstream curl | `MULTICA_GITHUB_REPO` unset on server | Set env; restart backend; check `/api/config` |
| Auto-update 404 `latest-mac.yml` | Mac desktop not on that tag | Manual publish to the **same** tag the updater targets |
| “Restart now” does nothing | Ad-hoc update ZIP vs Developer ID install | Ship Developer ID (+ notarized) builds only |
| Gatekeeper “could not verify” | Unnotarized or ad-hoc DMG | Notarize + staple; or Right-click → Open once |
| GHCR push fails on tag | Uppercase `Git-on-my-level` in image ref | Lowercase owner in docker publish |
| Windows desktop CI fails | `-c.publish.repo=` misparsed on Windows | Use `--config.publish.*` + `shell: bash` |
| Spotlight full of Multica backups | Failed ShipIt / updater | `bash scripts/cleanup-macos-desktop-updater.sh` |

## Desktop auto-update

Installed apps look at `Git-on-my-level/multica` releases (baked `app-update.yml`
from CI `--config.publish.owner`). The committed `electron-builder.yml` default
stays `multica-ai`; CI always overrides. The feed file is `latest-mac.yml` on
the **latest** release tag. Publishing Mac assets only to an older tag (e.g.
0.3.44) while CLI is on 0.3.45 causes 404s.

Signing rules of thumb:

- Ad-hoc builds: Gatekeeper blocks; cannot replace a Developer ID install in-place.
- Developer ID without notarization: Right-click → Open works; browser download still noisy.
- Notarized + stapled: clean install and auto-update.

Wire fork repo secrets before relying on CI Mac builds: `CSC_LINK`,
`CSC_KEY_PASSWORD`, `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`.
Details: [`apps/desktop/MACOS_RELEASE.md`](apps/desktop/MACOS_RELEASE.md).

## Syncing from upstream

```bash
git remote add upstream https://github.com/multica-ai/multica.git   # once
./scripts/sync-upstream.sh   # fetch + report ahead/behind (no auto-merge)
git fetch upstream
git checkout main
git merge upstream/main
```

Expect to re-apply or keep:

- `scripts/install-fork.*` → `FORK_DEFAULT_GITHUB_REPO` / `$ForkDefaultGithubRepo` (thin overlay for bare curl|bash)
- All **fork-only files** above (should not conflict if upstream never adds them)
- Env-guarded install/CLI changes usually merge cleanly

After merge, run a quick install-fork smoke test and confirm `/api/config` still
returns the fork repo on the hosted backend.

## Help menu / docs URLs

Help → Docs defaults to `https://multica.ai/docs` even on this fork (product
concepts). Help → Changelog uses this fork's GitHub Releases when
`MULTICA_GITHUB_REPO` is set. Override with `MULTICA_DOCS_BASE_URL` /
`MULTICA_CHANGELOG_URL` if needed.

## Related PRs (history)

| PR | Topic |
|----|--------|
| [#5](https://github.com/Git-on-my-level/multica/pull/5) | Fork install + release pipeline + CLI update targeting |
| [#6](https://github.com/Git-on-my-level/multica/pull/6) | Fork desktop CI matrix + updater signature UX |
| [#7](https://github.com/Git-on-my-level/multica/pull/7) | arm64-only Mac CI + updater cleanup script |
| [#8](https://github.com/Git-on-my-level/multica/pull/8) | Install bin dir (`/opt/homebrew/bin`) + fail on error |
| [#9](https://github.com/Git-on-my-level/multica/pull/9) | Fork runbook + macOS release notes |
