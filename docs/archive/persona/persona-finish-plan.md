# Persona-integration — færdiggørelses-plan

**Mål:** lukke alle 19 punkter i `persona-deferred-work.md` + de tre
in-flight commitments (E1/E2/E3) fra `persona-next-session.md`. Hver
delivery har en eksplicit eval (kommando eller script) der skal være
grøn før punktet anses for færdigt. Ingen item lukkes baseret på "det
ser ud til at virke".

Planen er ikke eksekveret — den er deliverable. Når den er godkendt,
arbejder vi os ned ad rækkefølgen.

## Disciplin der forhindrer "bygget 5 gange uden at blive færdigt"

1. **Eval-first.** Skriv evalen FØR koden. Hvis evalen ikke kan formuleres
   som en kommando der returnerer 0/1, er kravet ikke skarpt nok.
2. **Et punkt ad gangen.** Næste item starter ikke før forrige items eval
   er grøn og committed. Ingen WIP-stak.
3. **Atomare commits pr. item.** Conventional format, scope-tag matcher
   item-nummer (`feat(persona): #9 token rotation UI`).
4. **`make check` + `scripts/e2e-persona.sh` skal være grøn efter hvert
   item** der ændrer integrationsoverflade.
5. **Subagents må kun lukke items de selv kan eval'e.** Jeg orchestrerer
   altid integrations-evaluen selv.

## Sekvens

Wave-rækkefølgen følger to akser: blokering for merge, og afhængighed
mellem items. Hver wave har en "wave-eval" der skal være grøn før næste
wave starter.

| Wave | Antal items | Estimat | Wave-eval |
|---|---|---|---|
| 1. Merge-blockere | 4 | ~5-7t | `make check` + `e2e-persona.sh` grøn, branchen kan mergres |
| 2. Sikkerhed + scanner | 5 | ~17-20t | `e2e-persona.sh` udvidet med scanner+MCP scenarios |
| 3. UX-completeness | 5 | ~10-13t | Manuel test-script gennem alle UI-flows + Playwright e2e |
| 4. Operations | 8 | ~17-22t | Audit + activity-feed scripts + drift-test |

**Total: 22 items (3 in-flight + 19 deferred), ~49-62 timer.**

---

# Wave 1 — Merge-blockere

Disse fire items skal lukkes før branchen kan presses. Wave 1 har én
samlet eval: hele `e2e-persona.sh` skal være grøn med `--runtime-persona-sandbox`-flowet
aktivt, og `go test ./...` grøn i begge repos.

## W1.1 — E1: runtime-sandbox upper bound (FÆRDIGGØR WIP)

**Status:** stort WIP eksisterer (~1000 linjer i diff på cerebro-side).
RuntimeResponse har `persona_sandbox` + `capabilities`, daemon's
`persona.go` udvidet, runtime_test.go + agent_test.go har nye tests.

**Hvad der mangler at verificeres:**
- Daemon-side enforcement: når både runtime.persona_sandbox og
  agent.persona_sandbox er sat, vinder runtime som hard upper bound
- API: `PUT /api/runtimes/{id}/persona-sandbox` (eller PATCH-variant)
  returnerer 200 for owner/admin, 403 for andre
- UI: dropdown på runtime-detail-side henter via `api.listPersonaSandboxes()`
- TaskAgentData sender `runtime_persona_sandbox` til daemon

**Eval:**
```bash
# Backend
cd server && go test -run 'TestPreparePersonaSpawn_RuntimeUpperBound' ./internal/daemon/
cd server && go test -run 'TestRuntimePersonaSandbox' ./internal/handler/

# Frontend
pnpm --filter @multica/views typecheck
pnpm --filter @multica/web typecheck

# E2E (nyt scenario tilføjes til e2e-persona.sh):
# Scenario: runtime=claude-readonly + agent=claude-power → Bash blokeres
PERSONA_E2E_RUNTIME_BOUND=1 scripts/e2e-persona.sh
```

**DONE når:** alle tre tests grønne, e2e-script's nye scenarie verificerer
at runtime vinder over agent.

**Estimat:** 2-3t (mest verifikation + e2e-udvidelse — koden er stort
set skrevet).

## W1.2 — E2: stram update-rettighed til workspace owner/admin

**Hvad:** I `internal/handler/agent.go` `UpdateAgent` — kræv
workspace-rolle owner/admin når `persona_sandbox` ændres. Samme
mønster som andre admin-only operationer i samme fil.

**Eval:**
```bash
cd server && go test -run 'TestUpdateAgent_PersonaSandbox_RequiresOwner' ./internal/handler/
# Skal dække: owner=200, admin=200, agent-owner-uden-workspace-rolle=403
```

**DONE når:** test grøn, fail-cases viser 403 i handler-output.

**Estimat:** 30m-1t.

## W1.3 — #4: e2e bruger `--runtime-persona-sandbox` (afhænger af W1.1)

**Hvad:** I `scripts/e2e-persona.sh`'s E1-sektion, erstat den nuværende
`persona_admin PUT /v1/actors/$ID/sandbox` blok med spawn der bruger
`--runtime-persona-sandbox claude-readonly` direkte. Det tester
den ægte daemon-cerebro_service-flow ende-til-ende.

**Eval:**
```bash
scripts/e2e-persona.sh
# Output skal indeholde: "E1: runtime upper bound enforced via --runtime-persona-sandbox"
# Og fortsat: "ALL ASSERTIONS PASSED"
```

**DONE når:** scriptet ikke længere kalder `persona_admin` for sandbox-assign,
og e2e er grøn.

**Estimat:** 30m.

## W1.4 — #3: JEH-194 pre-eksisterende test-fejl

**Hvad:** `TestStartTask_AutopilotRunOnlyTask_ResolvesWorkspace` og
`TestClaimTask_AutopilotRunOnly_PopulatesWorkspaceID` i
`server/internal/handler/daemon_test.go` er røde. Find lookup-bugen for
autopilot run-only tasks (issue_id NULL, kun autopilot_run_id) og fix
resolveren så den tager begge stier.

**Eval:**
```bash
cd server && go test -run 'TestStartTask_AutopilotRunOnlyTask_ResolvesWorkspace|TestClaimTask_AutopilotRunOnly_PopulatesWorkspaceID' ./internal/handler/
# Begge grønne
```

**DONE når:** begge tests grønne, samt fuld `go test ./internal/handler/`
grøn.

**Estimat:** 1-2t.

## Wave 1 — samlet eval

```bash
# Cerebro-siden
cd /Users/hvejsel/firtal-repos/firtal-cerebro-persona-integration
make check                      # typecheck + tests + e2e
scripts/e2e-persona.sh          # ALL ASSERTIONS PASSED

# Persona-siden
cd /Users/hvejsel/firtal-repos/firtal-persona-cerebro-integration
go test ./...
```

Først når begge er grønne kan branchen presses.

---

# Wave 2 — Sikkerhed + scanner-integration

Det største og vigtigste arbejde. Indeholder #1 (scanner-integration —
det "store hul" hvor jeg påstod jeg gjorde noget jeg ikke gjorde) og
fundamentet (#17 MCP-gating) der lukker faktiske sikkerhedshuller.

## W2.1 — E3 daemon-side: rapportér capabilities ved registrering

**Hvad:** Daemon (cerebro) sender capabilities-payload ved registrering
til cerebro-server, som gemmer på `agent_runtime.capabilities` via
`UpdateAgentRuntimeCapabilities`. Statisk liste pr. provider er nok
til MVP.

```json
{
  "tools": ["Bash", "Write", "Edit", "Read", "Glob", "Grep"],
  "providers": ["claude"],
  "mcp_servers": []
}
```

**Eval:**
```bash
cd server && go test -run 'TestDaemonRegister_PopulatesCapabilities' ./internal/handler/
# Tjekker at registrering med en claude-runtime gemmer tools-array i capabilities-felt

# Manuel: starter daemon, slå op i DB
psql $MULTICA_DB -c "SELECT name, capabilities FROM agent_runtime WHERE provider='claude'"
# Forventer: capabilities indeholder mindst {"tools": ["Bash", ...], "providers": ["claude"]}
```

**DONE når:** test grøn, DB viser ikke-tom capabilities for hver registreret
claude-runtime.

**Estimat:** 2-3t.

## W2.2 — Cerebro: `GET /api/scanner-discovery/runtimes` public endpoint

**Hvad:** Read-only endpoint på cerebro der returnerer alle runtimes
på tværs af workspaces:
```json
[
  {"name": "claude-MacBook-Pro-8", "provider": "claude",
   "tools": ["Bash", ...], "mcp_servers": []}
]
```
Ikke workspace-scoped fordi persona's scanner kører cross-workspace.
Kræver service-token authentication (samme som cerebro_service-tokenen
persona allerede bruger).

**Eval:**
```bash
cd server && go test -run 'TestScannerDiscoveryRuntimes' ./internal/handler/
# 200 + array når token er gyldig service-token
# 401 uden token

# Manuel:
curl -H "Authorization: Bearer $CEREBRO_SERVICE_TOKEN" http://localhost:8484/api/scanner-discovery/runtimes | jq length
# Forventer: > 0
```

**DONE når:** test grøn, manuel curl returnerer alle aktive runtimes.

**Estimat:** 1-2t.

## W2.3 — #1: Persona-scanner forbruger runtime-discovery

**Hvad:** I persona-repo:
1. `scanner-targets.yaml` får nyt felt `runtime_discovery_url` på cerebro-app entry
2. Ny fil `internal/scanner/runtime.go` — parser discovery-output, indrapporterer
   hver runtime som governed surface eller "needs grant"
3. `data/scan-report.yaml` får ny sektion pr. runtime med tools-status
   (governed/ungoverned)
4. `/gaps`-side viser pr. runtime hvor mange tools er ungoverned i den
   aktive sandbox

**Eval:**
```bash
cd /Users/hvejsel/firtal-repos/firtal-persona-cerebro-integration
go test -run 'TestRuntimeScanner' ./internal/scanner/

# Integration: kør scanner mod live cerebro
go run ./cmd/persona scan --target firtal-cerebro
grep -A 5 'name: firtal-cerebro' data/scan-report.yaml | grep 'claude-MacBook'
# Forventer: hver runtime listet med tools governed/ungoverned status

# UI:
# Manuel: http://localhost:3001/gaps → se "Claude (MacBook-Pro-8) — N tools, M ungoverned"
```

**DONE når:** scan-report.yaml viser per-runtime sektion med ægte tools
(ikke "IKKE STYRET ENDNU"), `/gaps`-side renderer pr. runtime, klik på
ungoverned tool fører til sandbox-form.

**Estimat:** 4-5t.

## W2.4 — #9: Token-listing + rotation UI

**Hvad:** På `/actors/{id}` i persona-UI, erstat read-only placeholder
i "What keys does {actor.name} have?" sektion. Backend har allerede
`tokens`-tabel og `RevokeToken`. Mangler:
- `GET /v1/actors/{id}/tokens` returnerer liste med prefix (ikke fuld værdi)
- `POST /v1/actors/{id}/tokens` opretter ny token (returnerer engangs-fuld-værdi)
- `DELETE /v1/tokens/{id}` revoker
- UI: liste + "Rotate" knap der kører revoke-old + create-new sekventielt

**Eval:**
```bash
cd /Users/hvejsel/firtal-repos/firtal-persona-cerebro-integration
go test -run 'TestTokenListing|TestTokenRotation' ./internal/api/

# UI playwright (ny test):
pnpm --filter ui test:e2e tokens.spec.ts
# Skal: list, create, rotate, revoke
```

**DONE når:** Go-tests grønne, Playwright-test simulerer rotation flow,
UI viser kun token-prefix (aldrig fuld værdi efter creation-respons).

**Estimat:** 2-3t.

## W2.5 — #17: MCP-tool gating

**Hvad:** Persona's hook gater pt kun `claude.tool.*`. Hvis hooken ser
`mcp__*` resource-kinds skal den tjekke mod sandbox-grants. Kræver
design af MCP resource-kind-konvention (fx `mcp.<server>.<tool>` eller
`tool.mcp.<server>.<tool>`).

Plan:
1. Beslut konvention i kort design-note (1-side)
2. Udvid `cerebro-persona-hook` til at gate `mcp__*` calls med ny
   resource-kind
3. Tilføj seed-grants på `claude-developer` og `claude-power` for
   relevante mcp-servers
4. Test scenarie i e2e: agent prøver `mcp__supabase__execute_sql`,
   blokeres af claude-readonly, virker på claude-developer

**Eval:**
```bash
# Hook
go test -run 'TestHookGatesMCP' ./internal/hook/

# E2E nyt scenarie:
PERSONA_E2E_MCP=1 scripts/e2e-persona.sh
# Skal vise: "MCP tool blokeret af claude-readonly", "MCP tool tilladt på claude-developer"
```

**DONE når:** hook gater mcp__* calls, e2e dokumenterer både blok og
allow-case.

**Estimat:** 4t.

## Wave 2 — samlet eval

```bash
# Persona scan-rapport ikke længere viser "IKKE STYRET ENDNU" for runtime tools
grep -c 'IKKE STYRET ENDNU' /Users/hvejsel/firtal-repos/firtal-persona-cerebro-integration/data/scan-report.yaml
# Forventer: 0 (eller kun for ikke-runtime resources)

# E2e med alle nye scenarier
PERSONA_E2E_RUNTIME_BOUND=1 PERSONA_E2E_MCP=1 scripts/e2e-persona.sh
# Skal: ALL ASSERTIONS PASSED
```

---

# Wave 3 — UX-completeness

Items der gør UI'en faktisk brugbar. Her kan subagents parallelliseres
da items er isolerede.

## W3.1 — #10: Suspend/Delete actor-knapper

**Hvad:** Wire de eksisterende disabled-knapper på `/actors/{id}` op
til `PATCH /v1/actors/{id}` `{status:"suspended"}` og
`DELETE /v1/actors/{id}`. Backend findes, policy findes.

**Eval:**
```bash
pnpm --filter ui test:e2e actor-suspend-delete.spec.ts
# Suspend: status badge ændres til "suspended", actor kan ikke spawn ny task
# Delete: kræver bekræftelses-dialog, redirect til /actors-liste, actor væk
```

**DONE når:** Playwright-test grøn for begge flows.

**Estimat:** 1t.

## W3.2 — #11: Søgning og filtrering på `/actors`

**Hvad:** Filter-bar på `/actors`-listen: type, status, owner, navn-substring.

**Eval:**
```bash
pnpm --filter ui test:e2e actors-filter.spec.ts
# Seed 25 actors med forskellige type/status, verificer hver filter-kombination
```

**DONE når:** test grøn, manuel UI-test viser at 25+ actors stadig er
brugbar at navigere.

**Estimat:** 2t.

## W3.3 — #8: Per-aktør grants UI

**Hvad:** På `/actors/{id}` "What can {actor.name} do?" sektion. Backend:
- `GET /v1/actors/{id}/grants` (per-actor, ud over sandbox)
- `POST /v1/actors/{id}/grants` tilføjer grant
- `DELETE /v1/actors/{id}/grants/{grant_id}` fjerner

UI: liste over per-actor grants, "Tilføj grant"-form (resource-kind +
action + pattern), revoke-button.

**Eval:**
```bash
go test -run 'TestActorGrants' ./internal/api/
pnpm --filter ui test:e2e actor-grants.spec.ts
# Add grant → /v1/check returnerer allow for den specifikke action
# Remove grant → /v1/check returnerer deny igen
```

**DONE når:** add/remove grant ændrer faktisk autoritets-svar fra `/v1/check`.

**Estimat:** 3-4t.

## W3.4 — #7: UI-afklaring agent vs runtime sandbox

**Hvad:** På agent-settings-tab i Multica web — vis badge når runtime-cap
er aktiv: *"Begrænset af runtime-cap: claude-readonly. Din valgte
sandbox bliver ignoreret."* Eller skjul agent-sandbox dropdown helt.
Forslag: behold dropdown men disable + tooltip.

**Eval:**
```bash
pnpm --filter @multica/views vitest run agents/components/tabs/settings-tab.test.tsx
# Test: med runtime.persona_sandbox sat → badge synlig, dropdown disabled
# Uden runtime-cap → dropdown enabled, intet badge
```

**DONE når:** vitest grøn, manuel browser-test viser klart visuelt
signal at runtime-cap vinder.

**Estimat:** 1t.

## W3.5 — #14: Sandbox-templates / inheritance

**Hvad:** På `/sandboxes/new` i persona — knap "Lav baseret på existing
sandbox" der pre-fylder grants fra valgt source-sandbox.

**Eval:**
```bash
pnpm --filter ui test:e2e sandbox-template.spec.ts
# Vælg claude-developer som template → ny sandbox har samme grants pre-fyldt
# Submit → ny sandbox eksisterer med initial grants matchende source
```

**DONE når:** Playwright-test grøn, manuel test verificerer at grants
faktisk kopieres.

**Estimat:** 2t.

---

# Wave 4 — Operations

## W4.1 — #5: Forældede aktører ved agent-rename

**Hvad:** Vælg (a) — Daemon ved spawn: hvis aktør med samme uuid8 men
anderledes slug findes, soft-delete den gamle. Workspace-scope check
forhindrer kollision.

**Eval:**
```bash
go test -run 'TestDaemonSpawn_SoftDeletesPreviousActor' ./internal/daemon/
# Setup: actor cerebro:agent:ceo-agent-72995498 i persona
# Rename agent CEO → CTO, spawn task
# Assert: ceo-agent-... soft-deleted, cto-agent-72995498 aktiv
```

**DONE når:** test grøn, manuel rename-flow verificerer adfærd.

**Estimat:** 2t.

## W4.2 — #19: Re-sync ved CLI-opgradering

**Hvad:** Heartbeat-loop tjekker CLI-version. Ved drift, re-rapportér
capabilities. Plus `/api/runtimes/{id}/refresh-capabilities`-endpoint
operatør kan trigge.

**Eval:**
```bash
go test -run 'TestRuntimeHeartbeat_DetectsCLIVersionDrift' ./internal/daemon/
# Mock: CLI version A → registrer; CLI bytter til B → heartbeat trigger refresh

# Manuel:
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8484/api/runtimes/$ID/refresh-capabilities
# DB: capabilities-felt har nyt updated_at
```

**Estimat:** 2t.

## W4.3 — #18: Cross-runtime tools-aggregat (`/tools`-side)

**Hvad:** Global side på persona der aggregerer på tværs af alle aktørers
capabilities: "Bash bruges af 4 runtimes, gates af 2 sandboxes".

**Eval:**
```bash
pnpm --filter ui test:e2e tools-aggregate.spec.ts
# Seed: 2 runtimes, hver med Bash+Write; en sandbox gater Bash
# Forventer: side viser "Bash: 2 runtimes, 1 sandbox gater den"
```

**Estimat:** 3t.

## W4.4 — #15: Bulk actions

**Hvad:**
- Tildel samme sandbox til flere agents (Multica web)
- Suspend alle agents på en runtime (Multica web)
- Slet alle aktører i bulk (persona) — kun for dev/test, kræver bekræftelse

**Eval:**
```bash
pnpm --filter @multica/views vitest run agents/components/bulk-actions.test.tsx
pnpm --filter ui test:e2e bulk-delete-actors.spec.ts
```

**Estimat:** 2-3t.

## W4.5 — #12: Aktivitets-feed pr. sandbox

**Hvad:** På `/sandboxes/{id}` — top-tabel "Blokeret N gange seneste
24t for aktører X, Y, Z". Backend: index på log-records eller
view-afledning over `logbog`.

**Eval:**
```bash
go test -run 'TestSandboxActivityFeed' ./internal/api/
pnpm --filter ui test:e2e sandbox-activity.spec.ts
# Seed: 5 deny-events for sandbox X, åbn /sandboxes/X
# Forventer: tabel viser 5 deny-events grupperet pr. aktør
```

**Estimat:** 4t.

## W4.6 — #13: Multica audit-log UI

**Hvad:** Når workspace-owner ændrer runtime-cap eller agent.persona_sandbox
skal det logges i `activity_log`-tabellen og vises i UI'en. Kun for
admin-only operationer på sandbox-tildeling.

**Eval:**
```bash
cd server && go test -run 'TestActivityLog_PersonaSandboxChange' ./internal/handler/
pnpm --filter @multica/views vitest run settings/audit-log.test.tsx
# Browser: ændre persona_sandbox → side viser entry indenfor 5s
```

**Estimat:** 3t.

## W4.7 — #16: Per-tool resource_pattern UI (designtungt)

**Hvad:** Pattern-syntaks for grants — fx `Bash(git *)`, `Read(/Users/*/safe-paths/*)`.
Kræver:
1. Design-note først (1-side): pattern-syntaks
2. Backend: `resource_pattern` validering + matcher (kan genbruge
   eksisterende glob-lib)
3. Hook: matchere tool-call resource mod pattern
4. UI: pattern-input med real-time preview "matches: ls, git status, NOT: rm -rf"

**Eval:**
```bash
go test -run 'TestResourcePattern_Match' ./internal/policy/
# Bash(git *) matcher "git status", matcher ikke "rm"
# Read(/safe/*) matcher "/safe/foo", matcher ikke "/etc/passwd"

pnpm --filter ui test:e2e sandbox-pattern-grant.spec.ts
# Operatør tilføjer grant Bash(git *), test that ls blokeres men git ok
```

**Estimat:** 6-8t.

## W4.8 — Verifikations-bundt: #2, #5(verifikation), #6

**Hvad:** Tre items der primært er verifikation:
- #2: workdir staleness ved daemon-crash (sandsynligvis allerede løst)
- #5 verification: post-implementation
- #6: workdir disk-leak spotcheck

**Eval:**
```bash
# #2: kill -9 daemon midt i task, restart, verificer workdir tom indenfor 30s
scripts/test-daemon-crash-cleanup.sh

# #6: spotcheck
du -sh /Users/hvejsel/multica_workspaces_*
# Note baseline. Kør scripts/test-gc.sh som spawner gamle tasks og verificerer GC fjerner dem.
```

**Estimat:** 1t (begge verifikationer).

---

# Subagent-strategi

| Wave | Subagent-brug | Hvorfor / hvorfor ikke |
|---|---|---|
| W1 | Nej | Tæt koblet kode + e2e som integration-eval, kræver én hånd på rattet |
| W2.1-W2.2 | Nej | Daemon + cerebro server-side er sammenkoblet |
| W2.3 | Ja, 1 subagent | Persona-side scanner — afgrænset filer, jeg kører integrations-evalen |
| W2.4-W2.5 | Ja, parallelt | Token-rotation og MCP-gating er uafhængige |
| W3 | Ja, 2-3 parallelle | Items er UI-isolerede, hver subagent får 1 item + Playwright-test |
| W4.1-W4.7 | Ja, parallelt hvor possible | Mest uafhængige; W4.5 og W4.7 kunne paralleliseres |
| W4.8 | Nej | Verifikation, ikke kode |

**Regel for subagent-output:** subagentens leverance = kode + ny eval-test
+ output fra at have kørt evalen og set den grøn. Hvis evalen ikke kan
køres (fx kræver live persona-server), skal subagenten levere
manuel test-script som jeg kører.

---

# Komplet eval-suite (kør efter alle waves)

```bash
# Cerebro-side
cd /Users/hvejsel/firtal-repos/firtal-cerebro-persona-integration
make check
PERSONA_E2E_RUNTIME_BOUND=1 PERSONA_E2E_MCP=1 scripts/e2e-persona.sh
pnpm exec playwright test e2e/persona/

# Persona-side
cd /Users/hvejsel/firtal-repos/firtal-persona-cerebro-integration
go test ./...
go run ./cmd/persona scan --target firtal-cerebro
grep -c 'IKKE STYRET ENDNU' data/scan-report.yaml  # ≤ baseline
pnpm --filter ui test:e2e
pnpm --filter ui typecheck

# Cross-cutting integrations-eval
scripts/full-integration-eval.sh   # nyt script — kører hele matrixen
```

`scripts/full-integration-eval.sh` (skal skrives som del af W1.1):
- Bringer alle services op (`persona-local-up.sh`)
- Tjekker hver runtimes capabilities-felt er ikke-tom
- Kører persona scan og asserter scan-report ikke har "IKKE STYRET" for runtime tools
- Spawner agent med runtime-cap, asserter blokering
- Spawner agent uden runtime-cap, asserter agent-sandbox vinder
- Roterer en token og verificerer den gamle ikke længere virker
- Kalder mcp__-tool og verificerer gating

---

# Management summary

Persona-cerebro-integrationen har 22 åbne items: 3 i flow (E1/E2/E3),
7 fra del 1 i deferred-listen, 12 fra del 2. Estimeret 49-62 timer.

**Værket er delt i 4 waves**:
1. **Merge-blockere** (~5-7t): færdiggør E1 (allerede WIP), strenge auth
   for sandbox-ændring, e2e-script bruger den rigtige flag, fix de røde
   tests fra JEH-194. Når disse er grønne kan branchen presses.
2. **Sikkerhed + scanner** (~17-20t): det største og mest værdifulde.
   Lukker det sikkerhedshul jeg påstod jeg lukkede (#1 scanner-integration),
   plus token-rotation og MCP-gating som er konkrete sikkerhedsmangler.
3. **UX-completeness** (~10-13t): suspend/delete-knapper, søgning på
   actors-listen, per-aktør grants — ting der gør UI brugbar.
4. **Operations** (~17-22t): aktivitets-feeds, audit-log, bulk-actions,
   pattern-grants — det der gør platformen drift-klar.

**Anbefaling:** lås Wave 1 og Wave 2 til denne branch (de er
færdiggørelse af det jeg påstod var bygget). Wave 3 og 4 splittes ud
som separate features og prioriteres efter operatør-feedback.

**Disciplin der forhindrer "5 gange uden at blive færdig":** eval-first
(skriv testen før koden), ét item ad gangen, ingen WIP-stak, hvert item
committes atomart med matching scope-tag, og hver wave har en
samlet eval der skal være grøn før næste starter.
