# Changelog — Forge: Slack + Archive + Tab Rebrand

## [Approved] - 2026-05-13

### Approved
- Spec approved by shivasymbl <sdevinarayanan@asymbl.com> at 2026-05-13T23:57:00+05:30
- Status: in-review → approved
- Ready for implementation via `/claude-spec:implement forge-slack-archive-rebrand`

## [1.1.0] - 2026-05-13 (Brand audit addendum)

### Added
- Phase 1b: Brand audit fixes for 10 confirmed seepage points found by jcodemunch + LSP scan
- CRITICAL: co-authored-by hook script in `repocache/cache.go:802` writes `multica-agent <github@multica.ai>` into agent git repos — every agent commit since v0.2.32 sync is misattributed. Tests also expect wrong value.
- B5: `app.setName("Multica")` in desktop `index.ts` creates WM_CLASS mismatch with `StartupWMClass: Forge` in electron-builder.yml
- B7: `apps/desktop/package.json` productName/description/homepage still say Multica
- B8: runtimes-page.tsx links to `https://multica.ai/docs/daemon-runtimes` (broken for Forge users)
- B9: ACP handshake sends `"multica-agent-sdk"` as client name to external agent processes
- 9 new `verify-patches.sh` checks (Section 7) — total patch count: 38
- FR-012 through FR-015 added to REQUIREMENTS.md P0 requirements

### Changed
- Executive summary expanded to include rebrand audit findings table
- Phase summary updated (+Phase 1b, total effort +2 hours)
- Test checklist updated with B1 repair tests
- verify-patches.sh target updated: 33 → 38

## [1.0.0] - 2026-05-13

### Added
- Initial project specification created
- REQUIREMENTS.md (PRD) — 3 features bundled: Slack webhook notifications, Archive relabel, browser tab rebrand fix
- ARCHITECTURE.md (technical design) — workspace-scoped Slack config, async fire-and-forget delivery, isolated Go package
- IMPLEMENTATION_PLAN.md — 6 phases, ~22 hours of work, full test class breakdown
- DECISIONS.md — 8 ADRs covering schema choices, async delivery, package isolation
- RESEARCH_NOTES.md — jcodemunch blast-radius evidence (79 files for IssueStatus), zen:analyze findings, Slack docs

### Research Conducted
- jcodemunch MCP blast radius analysis on IssueStatus (79 files, risk 0.59), BOARD_STATUSES (11 files), STATUS_CONFIG (contained)
- zen:analyze (gpt-5.4, high thinking mode) architectural validation — confirmed Slack hook point + relabel-only approach for Archive
- Direct codebase reading: notification_listeners.go, issue.go, status.ts, layout.tsx, favicon.svg, integrations-tab.tsx
- Slack Incoming Webhooks API documentation reviewed
- Existing patterns analyzed: event bus, settings tab cards, RBAC middleware, TanStack Query workspace-scoped data

### Key Decisions (see DECISIONS.md)
- ADR-001: Slack config is workspace-scoped, not per-user
- ADR-002: Async fire-and-forget delivery (5s timeout)
- ADR-003: New `internal/integrations/slack/` package, minimal hook in existing files
- ADR-004: Plural-friendly schema (one webhook per workspace via partial unique index)
- ADR-005: Relabel `cancelled` → "Archive" in UI only, no DB change
- ADR-006: Plain text Slack message in v1, Block Kit deferred
- ADR-007: Webhook URL plaintext storage with mitigations (mask, SSRF prevent)
- ADR-008: Tab rebrand bundled in this PR

### Status
- `draft` → `in-review` (2026-05-13)
- Awaiting `/claude-spec:approve` before implementation

### Out of Scope (deferred)
- Slack Bot Token DMs to users (separate feature)
- Multiple Slack channels per workspace (table supports, UI deferred)
- Per-user Slack notification preferences (architectural anti-pattern, see ADR-001)
- Encryption of webhook URL at rest (system-wide concern, not Slack-specific)
- Custom workspace-defined statuses (workflow engine project, see RESEARCH_NOTES.md zen finding #3)
- Email or Microsoft Teams integrations (different listeners, same pattern)
- Slack messages on events other than status change (defer until v1 proves value)
