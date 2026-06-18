# Customizations

This file is the root-level ledger for behavior that may diverge from an official Multica implementation or image. Read it with `CLAUDE.md` before rebasing, updating official Docker images, or changing self-hosting assets.

## Rules

- Keep custom behavior documented in this file in the same PR that changes it.
- Prefer small, named settings over hidden behavior so future upgrades can preserve intent.
- When official images or upstream compose files change, compare this ledger before replacing local files.
- Keep Docker overrides explicit. If a local compose override, image tag, or Dockerfile patch becomes part of the deployment, list the file path and reapply checklist here.

## Active Customizations

### STR-170 — Auto-label new member-created issues

Status: in progress on branch `agent/builder/STR-170-auto-label-docs`

Purpose: When a workspace admin enables `workspace.settings.auto_label_new_issues`, newly created member-authored issues receive up to two labels. Existing labels are preferred; when the matcher finds a confident category and no matching label exists, the server creates the label and attaches it.

Touched areas:

- `server/internal/service/issue_auto_label.go` — auto-label service, deterministic matcher, label creation/attachment, event payloads.
- `server/cmd/server/issue_auto_label_listeners.go` — `issue:created` listener for member-authored issues.
- `server/pkg/db/queries/issue_label.sql` — case-insensitive label lookup by name.
- `packages/core/workspace/settings.ts` — typed helper for `auto_label_new_issues`.
- `packages/views/settings/components/workspace-tab.tsx` — Settings → General toggle.
- `packages/views/locales/*/settings.json` — localized settings copy.

Reapply checklist after upstream updates:

1. Confirm `issue_label` still enforces case-insensitive unique names and `issue_to_label` still has workspace-guarded attachment.
2. Confirm `IssueService.Create` still emits `protocol.EventIssueCreated` after commit.
3. Confirm `protocol.EventLabelCreated` and `protocol.EventIssueLabelsChanged` payload shapes still match frontend realtime expectations.
4. Confirm workspace settings still round-trip as JSON through `UpdateWorkspace`.
5. Run backend labeler tests and `packages/views` settings tests.

### Self-host Docker image customization

Status: existing supported local-build path

Purpose: Keep local/custom images separate from official GHCR images so an official image update can be pulled without losing local build behavior.

Current files:

- `docker-compose.selfhost.yml` pulls official images by default:
  - `${MULTICA_BACKEND_IMAGE:-ghcr.io/multica-ai/multica-backend}:${MULTICA_IMAGE_TAG:-latest}`
  - `${MULTICA_WEB_IMAGE:-ghcr.io/multica-ai/multica-web}:${MULTICA_IMAGE_TAG:-latest}`
- `docker-compose.selfhost.build.yml` builds local custom images:
  - `multica-backend:dev` from `Dockerfile`
  - `multica-web:dev` from `Dockerfile.web`
- `make selfhost-build` is the intended local/custom image path.

Reapply checklist after official image updates:

1. Pull or inspect upstream changes to `docker-compose.selfhost.yml`, `Dockerfile`, and `Dockerfile.web`.
2. Keep custom build tags distinct from official tags unless intentionally publishing a release image.
3. Run `docker compose -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml config` before starting the stack.
4. Start with `make selfhost-build` and verify backend `/api/config`, login, and a basic issue create flow.
5. If deployment needs an additional override file, commit it or document the exact local path and required env vars here.
