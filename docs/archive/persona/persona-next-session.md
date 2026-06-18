# Persona-integration — næste session

**Til en ny Claude Code-session der skal fortsætte hvor vi slap.** Læs hele filen før du starter.

## Hvor vi er

Persona-integrationen er i drift lokalt og fungerer end-to-end. Operatøren kan tildele en sandbox til en agent fra Multica's web-UI, og daemon's spawn-flow gates Bash/Write/Edit/etc gennem persona's hook. Activity-log virker. e2e-script er grønt.

Tre udvidelser er besluttet og delvist klargjort:

| ID | Hvad | Status |
|---|---|---|
| E1 | Runtime-level sandbox som hard upper bound | DB-skema + queries committet, ingen funktionel kode endnu |
| E2 | Stram update-rettighed til workspace owner/admin | Ikke startet |
| E3 | Per-runtime capability-discovery | DB-felt findes (capabilities JSONB), intet andet |

## Stack-status (alt verificeret kørende)

| Service | URL | Hvor startes |
|---|---|---|
| Cerbos | http://localhost:3593 | docker `persona-cerbos` |
| Persona API | http://localhost:4500 | `go run ./cmd/persona` i persona worktree |
| Persona UI | http://localhost:3001 | `pnpm dev` i persona worktree's `ui/` |
| Multica server | http://localhost:8484 | `go run ./cmd/server` med `MULTICA_PERSONA_*` env-vars |
| Multica web | http://localhost:3434 | `pnpm next dev --port 3434` med `REMOTE_API_URL=http://localhost:8484` |

Bring alt op idempotent: `scripts/persona-local-up.sh` (forudsætter `.env` kopieret fra main checkout). Bring ned: `scripts/persona-local-down.sh`.

Login: `888888` virker som master-kode for enhver email lokalt (kontrolleret af `MULTICA_DEV_MASTER_CODE` i `.env`).

## Git-state

To grene, intet pushet:

```
firtal-persona-cerebro-integration/  (worktree)
  branch: phase-A-cerebro-integration
  11 commits ahead of origin/main
  Indeholder: sandboxes datamodel, system:cerebro service-actor,
  /v1/sandboxes CRUD, sandbox-profile endpoint, /v1/check rewrite,
  UI for sandboxes, SDK additions

firtal-cerebro-persona-integration/  (worktree)
  branch: phase-B-persona-integration
  ~14 commits ahead of origin/main
  Indeholder: e2e-script, hook-binary, e2e-spawn CLI, persona_sandbox
  på agent (DB+API+daemon+UI), persona_sandbox på runtime (DB only),
  master-kode for lokal dev, lokal-up/down scripts
```

## E1 — det der er forberedt

Migration 9008 er kørt på din lokale DB og added to git:
- `agent_runtime.persona_sandbox TEXT NULL`
- `agent_runtime.capabilities JSONB DEFAULT '{}'`

Sqlc-genererede queries:
- `UpdateAgentRuntimePersonaSandbox(id, sandbox_name)` — tom streng = clear
- `UpdateAgentRuntimeCapabilities(id, jsonb)` — for E3

## E1 — det der mangler

1. **AgentRuntime-response shape**: `RuntimeResponse` i handler skal eksponere `persona_sandbox` (eksisterer ikke endnu — find filen `internal/handler/runtime.go` eller hvor runtime-responses bygges)

2. **API endpoint til at sætte det**: enten `PATCH /api/runtimes/{id}` eller dedikeret `PUT /api/runtimes/{id}/persona-sandbox`. Authoriseret som workspace owner/admin (samme middleware som eksisterende runtime-management)

3. **TaskAgentData-struktur** sender pt agent.persona_sandbox til daemon. Tilføj `runtime_persona_sandbox` så daemon ved hvilken upper bound der gælder

4. **Daemon enforcement**: i `internal/daemon/persona.go` — `preparePersonaSpawn` skal modtage runtime-sandbox også og vælge den mere restriktive. Mest-restriktive defineres som: hvis runtime sætter X og agent sætter Y, brug det sandbox hvis grants er en subset af det andet, ellers brug runtime (det er den hårde grænse). Simpelt fail-safe: brug altid runtime-sandbox hvis sat — den agent-tildelte ignoreres

5. **UI**: ny tab eller sektion på runtime-detail-siden i Multica web. Find `packages/views/runtimes/components/` — der bør være en runtime-detail. Tilføj samme dropdown-mønster som på agent-settings-tab. Brug eksisterende `api.listPersonaSandboxes()`

6. **Tests**:
   - Go-test der mocker både runtime og agent med forskellige sandboxes, verificerer daemon vælger upper bound
   - e2e-persona.sh udvidelse: tildel claude-readonly på runtime, claude-power på agent → forventer Bash blokeret

## E2 — opgavens helhed

Find i `internal/handler/agent.go`:

```go
func (h *Handler) UpdateAgent(...) {
    ...
    if req.PersonaSandbox != nil { ... }
    ...
}
```

Tilføj før det at hvis `persona_sandbox` er i requestet, kræv workspace-rolle owner/admin (ikke bare agent owner). Bruger sandsynligvis eksisterende `requireWorkspaceRole` helper med kun `"owner", "admin"` i listen — som det gøres andre steder i samme fil for admin-only operationer.

Skriv en test der dækker:
- Workspace owner: kan ændre persona_sandbox
- Agent owner (men ikke workspace admin): får 403

## E3 — design

Capabilities-feltet er i DB. Mangler:

1. **Daemon-side reporting**: i `cmd/multica/cmd_daemon*.go` ved registrering, send et payload der inkluderer:
   ```json
   {
     "tools": ["Bash", "Write", "Edit", ...],   // claude-specific
     "providers": ["claude"],
     "mcp_servers": []   // hvis nogen er konfigureret i daemon's MCP config
   }
   ```
   Statisk liste pr. provider er nok til MVP. Gem på `agent_runtime.capabilities` via `UpdateAgentRuntimeCapabilities`

2. **API**: udvid `RuntimeResponse` til at inkludere `capabilities`. UI kan så vise det

3. **Persona UI**: på `/sandboxes/[id]`-siden vis "Hvilke runtimes har de tools du gater?" — brug data fra Cerebro's `/api/runtimes` capabilities. Cross-system query, men giver operatøren konkret svar på "kan jeg trygt give claude-developer til denne agent på denne runtime?"

4. **Operatør-flow**: når operatør opretter en NY sandbox i persona, vis suggestion "din claude-runtime understøtter disse tools" så de kan vælge fra en konkret liste

E3 er den største — start med 1+2 (daemon + API), 3+4 kan udskydes.

## Hvad du IKKE skal gøre

- Push noget til remote uden eksplicit OK
- Røre brugerens main checkouts (`/Users/hvejsel/firtal-repos/firtal-{cerebro,persona}/`)
- Køre `make dev` eller andet der overskriver eksisterende state
- Ændre på det der allerede er committet hvis ikke nødvendigt — fundamentet er testet og stabilt

## Verifikation efter du er færdig

1. `go test ./...` passerer i begge repos
2. `pnpm --filter @multica/views typecheck` og `--filter @multica/web typecheck` passerer
3. `scripts/e2e-persona.sh` ALL ASSERTIONS PASSED
4. Browser-test: åben localhost:3434, login `888888`, gå til en agent's settings, dropdownen er der. Gå til en runtime, NY dropdown er der.
5. Sæt `claude-power` på agent + `claude-readonly` på runtime → spawn et task → verificér Bash blokeres (runtime upper bound vandt)

## Noter / faldgruber jeg ramte

- Multica's web kører på port 3434 (`FRONTEND_PORT`), IKKE Next.js' default 3000
- `REMOTE_API_URL=http://localhost:8484` skal sættes ellers proxier `/api/*` til 8080
- pnpm hoister ikke lightningcss native module korrekt — `local-up.sh` patcher det
- API endpoints under "auth+task-allowlist"-gruppen kan ikke nås med Bearer-token alene; bruger-routes skal være i den ANDEN auth-gruppe (omkring line 227 i `cmd/server/router.go`)
- Cerbos rejecter multi-document YAML — én resource pr. fil
- Soft-deleted aktører i persona kan ikke genoprettes med samme navn (UNIQUE-constraint på navn) — e2e-script bruger RUN_TAG-suffix

## Memory

Persona er bevidst valgt som SEPARAT service (ikke konsolideret ind i Multica) per operatør-beslutning. Ikke omgør det.
