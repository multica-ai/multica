# Skill-importing source map

Evidence layer for `multica-skill-importing`; paths relative to repo root
(`multica/`). Re-derive before trusting lines; re-verify via grep:

```bash
grep -n "func (h *Handler) ImportSkill" server/internal/handler/skill.go
grep -n "func runSkillImport"           server/cmd/multica/cmd_skill.go
grep -n "func IsReservedContentPath"    server/internal/skill/reserved.go
```

## Import endpoint route

| Behavior | File:line |
|---|---|
| `ImportSkill` handler (`POST /api/skills/import`) | `server/internal/handler/skill.go:1882` |
| Decodes `ImportSkillRequest`; validates `on_conflict` | `server/internal/handler/skill.go` |
| Source detection + URL normalization | `skill.go:1910` (`detectImportSource`) |
| Provenance into `config.origin` | `skill.go:1944-1948` |
| Structured conflict dispatcher | `skill.go:1813-1878` |
| Builds skill + files via `createSkillWithFiles` | `server/internal/handler/skill_create.go` |
| Success: `201` `{status:"created", skill}` (`on_conflict` sent) / bare `SkillWithFilesResponse` (omitted) | skill.go |
| Route `r.Post("/import", h.ImportSkill)` | `server/cmd/server/router.go:874` |

Note: `ImportSkill` branches on content type — multipart → archive path, JSON →
URL flow; both converge on `finishSkillImport`.

## Local archive (`.skill` / `.zip`)

| Behavior | File:line |
|---|---|
| Multipart branch (`isMultipartForm` :26), `importSkillFromArchive` (`MaxBytesReader`+`file`) | `server/internal/handler/skill_import_archive.go` |
| Upload cap `maxImportArchiveUploadSize` (16 MiB) | `skill_import_archive.go` |
| `parseSkillArchive`: zip decode, shallowest-`SKILL.md` root, frontmatter, zip-slip | `skill_import_archive.go` |
| Caps via `importedSkill.addFile`; name fallback (dir → filename); ignore dotfiles/`__MACOSX`/license; per-entry cap :234 | `skill_import_archive.go` |

## CLI `--url` / `--file`

| Behavior | File:line |
|---|---|
| Command def | `server/cmd/multica/cmd_skill.go:59-63` |
| Flags: `--url` (143), `--file` (144, exclusive), `--on-conflict` `fail` (145), `--output` `json` (146) | cmd_skill.go |
| `runSkillImport` (412); exactly one source (420-427) | cmd_skill.go |
| `--file` → multipart `ImportSkillFile`; URL → `POST /api/skills/import` (455) | cmd_skill.go |
| Prints structured result (443) | cmd_skill.go |

## Same-name conflict handling

| Behavior | File:line |
|---|---|
| `SkillImportResult` / `ExistingSkillIdentity` (`id`, `name`, `created_by`, `can_overwrite`) | skill.go |
| Pre-create lookup (1951-1962) + race-safe unique-violation fallback | skill.go |
| Default `fail`: `status:"conflict"` + HTTP 409 (1872-1877) | skill.go |
| `overwrite`: creator-only, preserves identity/bindings (`overwriteSkillWithFiles`) | skill.go |
| `rename`: suffixed name, bounded attempts (185) | skill.go |
| `skip`: `status:"skipped"`, untouched | skill.go |
| Legacy (no `on_conflict`): `skill.go:19`, body `{error, existing_skill}`; CLI normalizes to `status:"conflict"` | skill.go, cmd_skill.go |

## Response shape

`SkillWithFilesResponse` = `SkillResponse` + `Files []SkillFileResponse`
(`skill.go:80-87`); `createSkillWithFilesInTx` returns `{SkillResponse,
Files}`; `config.origin` on import (1947). In CLI imports it sits under
`SkillImportResult.skill` (`created`/`updated`); legacy without
`on_conflict` gets a bare response.

## URL families (`detectImportSource`)

| Source | File:line |
|---|---|
| `detectImportSource` | `server/internal/handler/skill.go:773-804` |
| `skills.sh` / `www.skills.sh` | 791-792 |
| `clawhub.ai` / `www.clawhub.ai` | 793-794 |
| `github.com` / `www.github.com` | 795-796 |
| Bare slug → ClawHub | 798-800 |
| `parseGitHubURL` handles `/tree/{ref}/...`, `/blob/{ref}/.../SKILL.md` | skill.go |

## Add vs set

`AddAgentSkills` = additive (no RemoveAll), `POST /api/agents/{id}/skills/add`
(router.go:851); `SetAgentSkills` = RemoveAll + re-add,
`PUT /api/agents/{id}/skills` (router.go:850). CLI: `agent skills add`
(`server/cmd/multica/cmd_agent.go`:797 → POST :8xx), `set` (772 → PUT :790), `list` (740 → GET :750).

## Reserved `SKILL.md` filename

`ContentFilename = "SKILL.md"` (`server/internal/skill/reserved.go:12`);
`IsReservedContentPath` cleans path, case-insensitive. Import/create path
silently SKIPS a reserved supporting file (`continue`); `UpdateSkill` PUT
`/api/skills/{id}` also silently skips; only `UpsertSkillFile`
(`PUT /api/skills/{id}/files`) rejects with 400. Reason: the daemon writes the
skill's `Content` to `SKILL.md` itself when preparing the execution
environment (`reserved.go:8-24`).
