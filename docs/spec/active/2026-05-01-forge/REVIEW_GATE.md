# Forge Phase 1 — Review Gate

**Date**: 2026-05-01
**Status**: Ready for review

## Changed Files (Phase 1)

Run this to see all Phase 1 changes:
```bash
git diff v0.0.1-fork --name-only | sort
```

Key categories:
- **Brand assets**: apps/web/public/brand/ (Asymbl logos, favicons, mark variants)
- **Web metadata**: apps/web/app/layout.tsx, apps/web/app/favicon.ico/route.ts
- **Email templates**: server/internal/service/email.go (both verification + invitation)
- **Logo component**: packages/ui/components/common/multica-icon.tsx (MulticaIcon → AsymblLogo)
- **Login/auth UI**: packages/views/auth/login-page.tsx
- **Connect-remote dialog**: packages/views/runtimes/components/connect-remote-dialog.tsx
- **Web UI strings**: packages/views/layout/app-sidebar.tsx, onboarding, workspace pages
- **Tailwind theme**: apps/web/app/globals.css (Asymbl light-mode colors)
- **Docker Compose**: docker-compose.selfhost.yml (forge-* naming)
- **Desktop app**: apps/desktop/ (com.asymbl.forge, Forge product name, icon)
- **CLI**: server/cmd/multica/ (Use: "forge"), server/internal/daemon/ (system prompts)
- **Spec/governance**: .github/CODEOWNERS, LICENSE.asymbl

## zen Review

```bash
# Run zen code review on all Phase 1 changes
zen codereview --diff v0.0.1-fork..HEAD
```

## codex Review

```bash
# Run codex CLI review
codex review --diff v0.0.1-fork..HEAD
```

## Known TODOs / Deferred Items

1. **Desktop app icons (.icns/.ico)**: PNG replaced. Full macOS/Windows icon sets need regeneration (see apps/desktop/DESKTOP_ICONS.md).
2. **pnpm install not run**: All TS errors in IDE are pre-existing (node_modules not installed). Run `pnpm install` from repo root to resolve.
3. **@multica/* package imports**: Intentionally NOT renamed — internal Node.js module names. Too much churn, breaks upstream cherry-pick of security fixes.
4. **Go module path**: Intentionally kept as `github.com/multica-ai/multica` — internal, not user-visible.
5. **Google OAuth**: Not yet configured (Phase 2 item).
6. **Remote agent daemons**: Ben Corpay and other droplets are Phase 2.
7. **Landing pages**: apps/web/app/(landing)/ — replaced with minimal placeholder (internal tool, no public landing needed).
8. **Release binary names**: cli/update.go asset names (multica-cli-*, multica_*) and GitHub release URL point at upstream repo — update when Forge gets its own release pipeline (Phase 2).
9. **Config directory migration**: Existing `~/.multica/` configs will not auto-migrate to `~/.forge/`. Users upgrading from a multica install need to copy manually.

## Best-Practices P2 Deferred Items

Flagged by best-practices review, intentionally deferred to Phase 2:

10. **Docker Compose resource limits**: No `deploy.resources.limits` set on any service. Add CPU/memory limits before production hardening to prevent runaway containers from starving the host.
11. **Postgres healthcheck `start_period`**: The postgres service healthcheck lacks a `start_period`, so compose may mark it unhealthy before the DB finishes initializing on first boot. Add `start_period: 30s` to the healthcheck block.
12. **MULTICA_* env var rename**: All 15 `MULTICA_*` env var keys in `.env.example` and the Go server are intentionally kept for backward compatibility. The Go server reads these via `os.Getenv("MULTICA_*")` throughout — renaming them requires a coordinated change across the Go server, all deployment configs, docker-compose, and any existing installs. Deferred to Phase 2. A clarifying comment has been added to `.env.example`.
13. **electron-builder.yml `hardenedRuntime: true`**: Missing from the mac build section. This flag is required for Apple notarization of the signed `.app`. electron-builder may enable it implicitly when `notarize` is configured, but explicit is safer. Add before the first production Mac release.
14. **electron-builder.yml `gatekeeperAssess: false`**: Missing from the mac build section. This is the standard pattern for direct-distribution (non-App-Store) signed builds — it skips the local Gatekeeper check during packaging, which can fail in CI environments. Add before the first production Mac release.

## Acceptance Criteria Verification

Before approving for Phase 2:
- [ ] Run: `grep -rn "Multica\|multica\.ai" apps/web/ packages/views/ server/cmd/ apps/desktop/src/ --include="*.tsx" --include="*.ts" --include="*.go" | grep -v "@multica\|from '@multica\|from \"@multica\|//\|test\|Test"` — should be near-zero results
- [ ] Email templates: send test email from dev mode, verify Asymbl brand renders
- [ ] Login page: shows "Sign in to Forge" with Asymbl logo
- [ ] Sidebar: shows Asymbl mark (not Multica asterisk)
- [ ] Connect-remote dialog: shows `forge daemon start` commands
- [ ] Docker compose: `docker compose ps` shows forge-* container names
- [ ] CLI: `forge --help` shows "Work seamlessly with Forge from the command line."
- [ ] CLI: `forge version` outputs `forge <version> (commit: ...)`
- [ ] Config: `forge setup self-host` writes config to `~/.forge/config.json`
- [ ] Agent system prompt: spawned agent sees `forge issue get` commands (not `multica`)
