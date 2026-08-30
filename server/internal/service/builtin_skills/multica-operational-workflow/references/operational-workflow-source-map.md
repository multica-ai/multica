# Operational workflow source map

Evidence layer for `SKILL.md`. Source locations identify the current
implementation context; re-check nearby code when lines move.

## Verification

```bash
go test ./internal/service -run TestOperationalWorkflowSkillContract
go test ./internal/handler -run 'TestOperationalWorkflowSkill(Data|Refs)IsModeGated'
```

## Mode and attachment

| Contract | Source | Behavior |
|---|---|---|
| Supported values and coding fallback | `server/internal/handler/agent_validation.go` | Only `coding`, `operational`, and `hybrid` are accepted; missing or unknown stored values normalize to `coding`. |
| Claim attachment gate | `server/internal/handler/daemon_operating_mode_skills.go` | The operational workflow built-in is removed for coding and unknown modes and retained for operational and hybrid modes. |
| Full and slim claim paths | `server/internal/handler/daemon.go`, `buildClaimedTaskResponse` | Both embedded skill data and skill references pass through the same mode decision. |

## Issue workflow

| Contract | Source | Behavior |
|---|---|---|
| Issue and comment reads | `server/cmd/multica/cmd_issue.go` | The CLI exposes JSON issue reads and root-first comment inspection. |
| File-backed comments | `server/cmd/multica/cmd_issue.go`; `server/internal/daemon/execenv/runtime_config_sections.go` | Agent-authored comments use a UTF-8 file and `--content-file`. |
| Honest completion | `server/internal/service/builtin_skills/multica-working-on-issues/SKILL.md` | Status and result reporting follow evidence from the actual task rather than an assumed outcome. |

## Authorization boundary

Operating mode is prompt and workflow configuration. Policy, tool
authorization, approvals, and execution controls remain separate and
authoritative. This skill does not change those systems.
