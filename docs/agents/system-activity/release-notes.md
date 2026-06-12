# Release Notes — System Activity Phase 1 (TECH-3038)

## Ændringer

### 1. Consecutive postpone counting
- Ny kolonne `consecutive_postpones int NOT NULL DEFAULT 0` på `cerebro_agent_wakeup` (migration 9065)
- Efter hvert dispatch: `IncrementWakeupPostpones`
- Når count >= 3: inbox-notifikation til issue-ejer + counter reset

### 2. Backend-settings
- `WakeupMinIntervalMinutes = 15` (minimum gap ved oprettelse + postpone-delay)
- `WakeupMaxConsecutivePostpones = 3` (maks consecutive dispatches)

### 3. Cleanup af hængende tasks
- Før wakeup-dispatch: `CancelAgentTasksByIssueAndAgent` fjerner aktive tasks
- Gælder UANSET om runtime er online/offline (tidligere kun offline)

### 4. Synlig agent-status i tråden
- Wakeup-comment type=`wakeup` → frontend viser som synlig besked, ikke skjult note
- (Implementeret i PR #972 — Phase 1 bygger videre)

### 5. Fjernelse af `recurring`/`interval_seconds`
- `trigger_type="recurring"` fjernet fra MCP-skema (`schedule_wakeup`)
- `interval_seconds` parameter fjernet fra MCP-skema
- Workspace CLAUDE.md opdateret (Rule 2 + Wakeup-sektionen)
- Mia agent-instructions opdateret
- Runtime-brief: nyt afsnit `## Wakeup ved ventetid` uden recurring

## Migrationer

| Fil | Indhold |
|---|---|
| `9065_cerebro_wakeup_phase1.up.sql` | ADD COLUMN consecutive_postpones + index |
| `9065_cerebro_wakeup_phase1.down.sql` | DROP INDEX + DROP COLUMN |

## Filer ændret

- `server/migrations/9065_cerebro_wakeup_phase1.{up,down}.sql`
- `server/internal/cerebro/queries/wakeup.sql` (3 nye queries + consecutive_postpones i RETURNING)
- `server/internal/cerebro/db/generated/models.go` (ConsecutivePostpones felt)
- `server/internal/cerebro/db/generated/wakeup.sql.go` (19-kolonne scan + 3 nye funktioner)
- `server/internal/cerebro/wakeup/service.go` (konstanter + dispatch-logik)
- `server/cmd/multica/cerebro_wakeup_mcp_tools.go` (fjern recurring + interval_seconds)
- `server/internal/daemon/execenv/runtime_config_wakeup_mandatory_cerebro.go` (nyt)
- `server/internal/daemon/execenv/runtime_config.go` (wir cerebroWakeupMandatoryRule)
