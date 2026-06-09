# Skill-importing source map

Evidence layer for `multica-skill-importing`. Every behavioral claim in `SKILL.md`
maps to a real code path below with `file:line`. Paths are relative to the repo
root (`multica/`).

Re-derive before trusting: line numbers drift. To re-verify a single anchor,
`grep` the symbol and read its surroundings, e.g.:

```bash
grep -n "func (h \*Handler) ImportSkill" server/internal/handler/skill.go
grep -n "func runSkillImport"           server/cmd/multica/cmd_skill.go
grep -n "func IsReservedContentPath"    server/internal/skill/reserved.go
```

## Import endpoint and route

| Behavior | File:line |
|---|---|
| `ImportSkill` handler (`POST /api/skills/import`) | `server/internal/handler/skill.go:1724` |
| Decodes `ImportSkillRequest` (`{ "url": ... }`) | `server/internal/handler/skill.go:1737-1741`, struct at `:523` |
| Detects source family + normalizes URL | `server/internal/handler/skill.go:1743` (calls `detectImportSource`) |
| Persists provenance into `config.origin` | `server/internal/handler/skill.go:1776-1781` — set at `:1780` **only when** `imported.origin != nil` (`:1779`); otherwise `config` stays `{}` and `origin` is absent |
| Builds skill + files via `createSkillWithFiles` (def `server/internal/handler/skill_create.go:72`, tx body `:27`) | call site `server/internal/handler/skill.go:1783` |
| Success: `201 Created` with the response body | `server/internal/handler/skill.go:1806` |
| Route registration `r.Post("/import", h.ImportSkill)` | `server/cmd/server/router.go:621` (under `/api/skills`, `:617`) |

## CLI: `multica skill import --url`

| Behavior | File:line |
|---|---|
| `skill import` command def | `server/cmd/multica/cmd_skill.go:60-64` |
| `--url` flag | `server/cmd/multica/cmd_skill.go:143` |
| `--output` flag (default `json`) | `server/cmd/multica/cmd_skill.go:144` |
| `runSkillImport` | `server/cmd/multica/cmd_skill.go:412` |
| Requires `--url` | `server/cmd/multica/cmd_skill.go:418-421` |
| `POST /api/skills/import` | `server/cmd/multica/cmd_skill.go:431` |
| On error, tries conflict handler; returns `nil` (exit 0) when handled | `server/cmd/multica/cmd_skill.go:432-434` |

## Duplicate 409 handling

| Behavior | File:line |
|---|---|
| `ImportSkill` unique-violation branch | `server/internal/handler/skill.go:1793-1800` |
| Structured 409 via `writeSkillImportDuplicateConflict` (looks up existing skill) | `server/internal/handler/skill.go:1794-1795` |
| `writeSkillImportDuplicateConflict` writes `{error, existing_skill}` at status 409 | `server/internal/handler/skill.go:109-114` |
| `ExistingSkillIdentity` (`{id,name}`) | `server/internal/handler/skill.go:104-107` |
| `existingSkillIdentityByName` (GetSkillByWorkspaceAndName) | `server/internal/handler/skill.go:130-142` |
| Defensive fallback: plain-string 409 when lookup misses (delete-race) | `server/internal/handler/skill.go:1796-1798` |
| CLI `handleSkillImportConflict` | `server/cmd/multica/cmd_skill.go:447` |
| Requires HTTP 409 + non-empty body | `server/cmd/multica/cmd_skill.go:449-451` |
| Requires `existing_skill` key (else `false` → non-zero exit) | `server/cmd/multica/cmd_skill.go:457-459` |
| Under `--output json`: prints body, returns `true` (caller exits 0) | `server/cmd/multica/cmd_skill.go:461-465` |

A legacy plain-string 409 (no `existing_skill` key) fails the `:457-459` guard,
so `handleSkillImportConflict` returns `false`, `runSkillImport` falls through to
`return fmt.Errorf("import skill: ...")` at `:435` → non-zero exit.

## Response shape: `SkillWithFilesResponse`

| Behavior | File:line |
|---|---|
| `SkillWithFilesResponse` = embedded `SkillResponse` + `Files []SkillFileResponse` | `server/internal/handler/skill.go:99-102` |
| `SkillResponse` fields (`id, workspace_id, name, description, content, config, created_by, created_at, updated_at`) | `server/internal/handler/skill.go:41-51` |
| `SkillFileResponse` fields | `server/internal/handler/skill.go:80-87` |
| `createSkillWithFilesInTx` returns `SkillWithFilesResponse{SkillResponse, Files}` | `server/internal/handler/skill_create.go:66-69` |
| `config.origin` set on import | `server/internal/handler/skill.go:1780` |

The import response is a `SkillWithFilesResponse` (not a bare `SkillResponse`):
it carries every `SkillResponse` field plus the `files` array.

## URL source families (`detectImportSource`)

| Behavior | File:line |
|---|---|
| `detectImportSource` | `server/internal/handler/skill.go:723-756` |
| `skills.sh` / `www.skills.sh` | `server/internal/handler/skill.go:743-744` |
| `clawhub.ai` / `www.clawhub.ai` | `server/internal/handler/skill.go:745-746` |
| `github.com` / `www.github.com` | `server/internal/handler/skill.go:747-748` |
| Bare slug (no host) defaults to ClawHub | `server/internal/handler/skill.go:750-753` |
| `parseGitHubURL` handles `/tree/{ref}/...` and `/blob/{ref}/.../SKILL.md` | `server/internal/handler/skill.go:1402-1455` (tree/blob check `:1415-1432`) |

## Additive add vs replace-all set

| Behavior | File:line |
|---|---|
| `AddAgentSkills` (additive: AddAgentSkill loop, no RemoveAll) | `server/internal/handler/skill.go:1978`; loop `:2009-2017` |
| Route `POST /api/agents/{id}/skills/add` | `server/cmd/server/router.go:598` |
| `SetAgentSkills` (replace-all: RemoveAllAgentSkills then re-add) | `server/internal/handler/skill.go:1923`; `RemoveAllAgentSkills` `:1955`; re-add `:1960-1968` |
| Route `PUT /api/agents/{id}/skills` | `server/cmd/server/router.go:597` |
| CLI `agent skills add` def ("without replacing existing assignments") | `server/cmd/multica/cmd_agent.go:125-130` |
| `runAgentSkillsAdd` → `POST .../skills/add` | `server/cmd/multica/cmd_agent.go:839`; POST `:860` |
| CLI `agent skills set` def ("replaces all current assignments") | `server/cmd/multica/cmd_agent.go:118-123` |
| `runAgentSkillsSet` → `PUT .../skills` | `server/cmd/multica/cmd_agent.go:814`; PUT `:832` |
| CLI `agent skills list` | `server/cmd/multica/cmd_agent.go:782`; GET `:792` |

## Reserved primary-content filename (`SKILL.md`)

| Behavior | File:line |
|---|---|
| `ContentFilename = "SKILL.md"` | `server/internal/skill/reserved.go:12` |
| `IsReservedContentPath` (cleans path, case-insensitive compare) | `server/internal/skill/reserved.go:25-27` |
| Import/create path: reserved supporting file is **silently skipped** (`continue`) | `server/internal/handler/skill_create.go:50-54` |
| `UpdateSkill` (PUT `/api/skills/{id}`) replace-files path: also silently skips | `server/internal/handler/skill.go:461-464` |
| `UpsertSkillFile` (PUT `/api/skills/{id}/files`): **rejects 400** "SKILL.md is reserved for the primary skill content" | `server/internal/handler/skill.go:1851-1854` |

Reason `SKILL.md` is reserved: the daemon writes the skill's `Content` to that path
itself when preparing the execution environment, so a supporting file may not also
claim it (`server/internal/skill/reserved.go:8-24`).

Behavior is path-shape-dependent. On **import or create** a manifest's `SKILL.md`
supporting file is dropped (it will not appear in the returned `files`), so the
import still succeeds — it does not 400. The hard 400 rejection fires only on the
dedicated single-file endpoint `PUT /api/skills/{id}/files`.
