# Persona-integration — fravalgt arbejde

**Til en ny session.** Liste over ting jeg sagde jeg byggede / ville bygge,
men hvor enten implementationen er ufuldstændig eller blev fravalgt uden
eksplicit tilladelse. Brutal-ærlig så vi kan gøre det færdigt næste gang.

Hver punkt har:
- **Hvad jeg sagde** — hvordan jeg beskrev det
- **Hvad der faktisk blev bygget** — den ærlige version
- **Det der mangler**
- **DONE-test** — hvordan vi måler at det er færdigt

---

## 1. Runtime-scanner integration (DET STORE HUL)

**Hvad jeg sagde:** "#1 — Persona scanner skal kende runtime tools." Jeg
markerede den som færdig.

**Hvad der faktisk blev bygget:** Kun *aktør-detalje-siden* viser tools.
Daemonen pusher `runtime_tools` til hver aktørs `attributes`-felt, og
`/actors/{id}` viser dem som chips. Det er kosmetisk pr. aktør.

**Det der mangler:** Persona's *egentlige scanner* — `internal/scanner/`
— ved stadig intet om runtimes. Outputtet i `data/scan-report.yaml`
viser stadig:

```yaml
- name: firtal-cerebro
  rest:
    - '(discovery): IKKE STYRET ENDNU (REST: discovery method unavailable)'
  agent-værktøjer:
    - 'tool.bash.run: IKKE STYRET ENDNU (Phase 8 ...)'
    - 'tool.file.read: IKKE STYRET ENDNU (Phase 8 ...)'
    ...
```

Scanner-output, `/gaps`-siden, og scan-rapporten ignorerer cerebro's
runtime-data. En operatør der åbner persona's scan-rapport kan stadig
ikke se hvilke tools cerebro's runtimes har.

**Sådan bygges det:**
1. Cerebro eksponerer `GET /api/scanner-discovery/runtimes` — public
   read-only endpoint der returnerer `[{name, provider, tools[],
   mcp_servers[]}]` for alle runtimes på tværs af workspaces.
2. Persona's `scanner-targets.yaml` får et nyt felt `runtime_discovery_url`
   for cerebro-app entry.
3. `internal/scanner/rest.go` (eller ny `runtime.go`) parser den
   discovery-output og indrapporterer hver runtime som en governed
   surface — eller som "needs grant" hvis ingen sandbox dækker tool'et.
4. Coverage-rapporten skal kunne sige *pr. runtime* hvilke tools der er
   ungoverned i den aktive sandbox.

**DONE-test:**
- `data/scan-report.yaml` viser firtal-cerebro med en separat sektion
  pr. runtime, hver med tools-status (governed/ungoverned)
- `/gaps`-siden viser "Claude (MacBook-Pro-8) — 12 tools, 0 ungoverned"
- Operatør kan klikke på en ungoverned tool og blive sendt til den
  sandbox-form-der-foreslår-tools (#16 fra forrige run, allerede bygget)

**Estimat:** ~4t. Kræver ændringer i begge repos.

---

## 2. Workdir-staleness ved daemon-crash

**Hvad jeg sagde:** "#2 — Workdir clear'es ved task-slut." Markeret færdig.

**Hvad der faktisk blev bygget:** En `finishHook` der kører i `defer` i
`runTask` og pusher attrs uden workdir til persona når task slutter.

**Det der mangler:** Hvis daemonen crasher midt i en task (kill -9, OOM,
strømsvigt), kører `defer` aldrig. Workdir bliver stående på aktøren
permanent. Næste gang en operatør kigger ser de en sti der peger på en
mappe der ikke længere eksisterer.

**Sådan fixes det:**
- Daemon-start i `refreshPersonaActorAttrs` pusher allerede tom workdir
  for alle persona-gated agents. Det rydder evt. stale workdirs efter
  crash. ← Det her *virker faktisk* allerede som bivirkning, så hullet
  er kun "mellem crash og næste daemon-start".
- For at lukke det helt: persona kunne timeout'e attribute-felter med
  TTL. Men det kræver schema-udvidelse og er overkill.

**DONE-test:**
- Manuelt: start task, `kill -9` på daemon, restart daemon, verificer
  aktørens workdir er tom indenfor 30s.
- Hvis det allerede virker via daemon-start refresh: dokumentér det og
  luk emnet.

**Estimat:** 30 min verifikation. Sandsynligvis allerede løst.

---

## 3. JEH-194 — pre-eksisterende test-fejl

**Hvad jeg sagde:** Skrev en issue, "ikke blokerende".

**Hvad der faktisk blev bygget:** Issue oprettet
([JEH-194](http://localhost:3434/jeh-b0edd870/issues/585eb377-62f9-45b9-8b76-fe8ea9a6e443))
men testene er stadig røde.

**Det der mangler:** `TestStartTask_AutopilotRunOnlyTask_ResolvesWorkspace`
og `TestClaimTask_AutopilotRunOnly_PopulatesWorkspaceID` fejler. Hvis
branchen merger med disse røde tests, er CI blokeret.

**Sådan fixes det:**
- Læs `daemon_test.go:1286` og find ud af hvor task-resolveren fejler at
  håndtere autopilot run-only tasks (issue_id NULL, kun
  autopilot_run_id).
- Fix lookuppen så den tager begge stier.

**DONE-test:** Begge tests grønne på `go test ./internal/handler/`.

**Estimat:** 1-2t.

---

## 4. CLI's `--runtime-persona-sandbox` bruges ikke i e2e

**Hvad jeg sagde:** Tilføjede flagget til `multica agent e2e-spawn` så
e2e-scriptet kunne simulere runtime-cap-overrider.

**Hvad der faktisk blev bygget:** Flagget findes og virker. Men
e2e-persona.sh bruger det ikke — i stedet bruger scriptet `persona_admin`
direkte til at re-assigne sandbox før spawn (fordi cerebro_service-token
oprindeligt ikke kunne assigne sandboxes).

**Det der mangler:** Cerbos-policy fixet jeg lavede (sandbox-admin.yaml,
`cerebro-service-can-assign-sandboxes`) gør at flagget NU kunne bruges.
E2e-scriptet skal opdateres til at bruge `--runtime-persona-sandbox`
flagget i stedet for at omgå med admin-token. Det er den ægte
end-to-end-test af E1-precedensen via daemon-flow.

**Sådan fixes det:**
- Erstat `persona_admin PUT /v1/actors/$ALLOWED_ACTOR_ID/sandbox` blokken
  i E1-sektionen af e2e-persona.sh med:
  ```sh
  PERSONA_SERVICE_TOKEN="$SVC_TOKEN" "$SPAWN_HELPER" agent e2e-spawn \
      --persona-actor "$ALLOWED_ACTOR_ID" \
      --runtime-persona-sandbox "claude-readonly" \
      --prompt "Run the command: ls -la"
  ```
- Sørg for at `e2e-spawn` faktisk kalder `daemon.AssignSandboxByName` med
  service-tokenen og at det får 200 (ikke 403).

**DONE-test:** e2e-persona.sh kører grønt med flagget i stedet for
admin-token.

**Estimat:** 30 min.

---

## 5. Forældede aktører efter agent-rename

**Hvad jeg sagde:** "Aktør-navn er nu slug + kort uuid for læsbarhed."

**Hvad der faktisk blev bygget:** Daemon laver aktører som
`cerebro:agent:<slug>:<uuid8>`. Hvis agent renames "CEO agent" → "CFO
agent" laver daemonen en NY aktør med navn `cerebro:agent:cfo-agent-<uuid>`,
og den gamle bliver hængende.

**Det der mangler:** Ingen migration når agent-navn skifter. Persona
ophober forældede aktører over tid.

**Sådan fixes det:** To valg:
- (a) Daemon ved spawn: hvis aktør med "samme uuid8 men anderledes slug"
  findes, soft-delete den gamle. Risiko: navnekollision på tværs af
  workspaces.
- (b) Persona-side garbage collection: aktører med 0 aktivitet i 30 dage
  soft-deletes automatisk.

**DONE-test:** Rename CEO agent → "CTO agent", spawn ny task, verificer
gammel `cerebro:agent:ceo-agent-72995498` er soft-deleted og ny
`cerebro:agent:cto-agent-72995498` er den eneste aktive.

**Estimat:** 2t (valg a) eller 4t (valg b).

---

## 6. Per-task workdir leaks fra daemon

**Hvad jeg sagde:** Ikke noget direkte.

**Hvad der faktisk blev bygget:** Daemon laver
`/Users/hvejsel/multica_workspaces_worktree-persona/<workspace>/<task>/workdir`
pr. spawn. Eksisterende GC kører hver time med 120t TTL — bør være ok,
men jeg har ikke verificeret.

**Det der mangler:** Spot-check at GC faktisk fjerner gamle workdirs.

**DONE-test:** Tæl `du -sh /Users/hvejsel/multica_workspaces_*` før og
efter en uge — bør ikke vokse monotonisk.

**Estimat:** 15 min spotcheck.

---

## 7. UI-afklaring: agent vs runtime persona-sandbox

**Hvad jeg sagde:** Bruger sagde "lad det være for nu". Markeret som
intentionelt fravalg.

**Det der mangler:** Brugeren spurgte om det er forvirrende at have
*begge* dropdowns. Nu er der bare en ufiltreret runtime-cap dropdown og
en ufiltreret agent-sandbox dropdown — uden visuelt signal om at
runtime'n vinder. Operatør kan sætte agent-sandbox til claude-power og
ikke forstå hvorfor det aldrig virker når runtime-cap'en er
claude-readonly.

**Sådan fixes det:**
- Vis et badge i agent-settings når en runtime-cap er aktiv:
  *"Begrænset af runtime-cap: claude-readonly. Din valgte sandbox bliver
  ignoreret."*
- Eller skjul agent-sandbox dropdown helt når runtime har en cap.

**DONE-test:** Sæt runtime-cap = claude-readonly, åbn en agent på den
runtime, se badge eller skjult dropdown.

**Estimat:** 1t.

---

## Kort kategori-oversigt

| # | Emne | Vægt | Estimat |
|---|---|---|---|
| 1 | Runtime-scanner integration | **Stort hul** — påstod jeg lavede det | 4t |
| 2 | Workdir efter crash | Sandsynligvis allerede løst, mangler verifikation | 30m |
| 3 | JEH-194 pre-eksisterende tests | Blokker for merge | 1-2t |
| 4 | E2e bruger ikke `--runtime-persona-sandbox` | Tester ikke daemon-flowet | 30m |
| 5 | Forældede aktører ved rename | Vil gradvis forurene persona | 2-4t |
| 6 | Workdir disk-leak | Sandsynligvis ok, mangler spotcheck | 15m |
| 7 | UI-afklaring agent/runtime sandbox | Bruger udskød eksplicit | 1t |

**Total estimat:** ~10-13 timer.

---

---

# Del 2 — Features der aldrig blev planlagt men som mangler

Ovenfor er ting jeg sagde jeg byggede. Nedenfor er ting der bare ikke
findes — placeholders i UI'en, halve flows, manglende admin-værktøjer.
Ikke fravalg, bare scope der aldrig kom på listen.

## 8. Per-aktør grants UI (read-only placeholder)

**Hvor:** `/actors/{id}` sektion *"What can {actor.name} do?"* viser
hardcoded tekst:
> "Permission management is read-only in this preview. Grant / revoke
> ships in a follow-up."

**Det der mangler:** Faktisk UI til at se og tilføje per-aktør grants
(ikke kun via sandbox). Backend har `actor_grants`-tabellen og
`POST /v1/actors/{id}/grants` skal bygges. Vigtigst når en aktør har
brug for én ekstra grant ud over sandboxens generelle.

**Estimat:** 3-4t.

## 9. Token-listing og rotation

**Hvor:** `/actors/{id}` sektion *"What keys does {actor.name} have?"*
samme read-only placeholder.

**Det der mangler:** Liste eksisterende tokens (med prefix, ikke fuld
værdi), rotation, revoke. Backend har `tokens`-tabellen og
`RevokeToken` findes — UI mangler. Kritisk for security-incident response
("vi tror cerebro_service er kompromitteret, roter den").

**Estimat:** 2-3t.

## 10. Suspend / Delete aktør-knapper

**Hvor:** `/actors/{id}` har de to knapper men de er disabled med
*"Coming in a follow-up"* tooltip.

**Det der mangler:** Wire dem op til `PATCH /v1/actors/{id}` med
`{status:"suspended"}` og `DELETE /v1/actors/{id}`. Backend findes,
policy findes. Bare action-handler + form + bekræftelses-dialog mangler.

**Estimat:** 1t.

## 11. Søgning og filtrering på /actors

**Hvor:** `/actors`-listen viser ALLE aktører usorteret.

**Det der mangler:** Filter på type, status, owner, navn-substring. Ved
mere end 20 aktører bliver siden ubrugelig.

**Estimat:** 2t.

## 12. Aktivitets-feed pr. sandbox

**Hvor:** `/sandboxes/{id}` viser kun grants og hvilke aktører der bruger
den. Ingen "hvad er denne sandbox blokeret for sidste 24t".

**Det der mangler:** Top af sandbox-detalje-side: tabel med "blokeret
N gange seneste 24t for aktører X, Y, Z". Skal hjælpe operatør med at
opdage at en sandbox er for restriktiv. Backend
(`/v1/log/search?sandbox_id=...`) findes ikke endnu — kræver index på
log-records eller view-afledning.

**Estimat:** 4t.

## 13. Audit-log: hvem ændrede hvad i Multica

**Hvor:** Persona har `logbog.jsonl` for tool-call decisions. Multica
logger via `slog.Info` — men kun til stdout/disk, ikke et
operatør-synligt feed.

**Det der mangler:** Når en workspace owner sætter runtime-cap
claude-readonly, eller ændrer agent.persona_sandbox, skal der være en
audit-trail i Multica's UI. Pt. ses det kun i serverloggen.

**Estimat:** 3t (eksisterende `activity_log`-tabel kunne bruges).

## 14. Sandbox-templates / inheritance

**Hvor:** Du kan kopiere claude-power og redigere, men ikke "arve"
fra den. Hver sandbox lever isoleret.

**Det der mangler:** *"Lav ny sandbox baseret på claude-developer"* —
clone grants automatisk så operatør kun skal trimme/tilføje. Reducerer
fejl ved sandbox-design.

**Estimat:** 2t.

## 15. Bulk actions

**Det der mangler:**
- Tildel samme sandbox til flere agents på én gang
- Suspend alle agents på en specifik runtime
- Slet alle aktører i bulk for ren-state-reset

Pt. er alt one-by-one i UI'en.

**Estimat:** 2-3t.

## 16. Per-tool resource_pattern UI

**Hvor:** Sandbox-form (#1 fra del 1) tilføjer grants som
`call-tool on claude.tool.bash` med `resource_pattern: "*"` — alle
kommandoer.

**Det der mangler:** UI til at refine pattern til fx
`Bash(git *)`-style — kun git-kommandoer tilladt. Eller
`Read(/Users/*/safe-paths/*)`. Pattern-syntaks skal designes først.

**Estimat:** 6-8t (designtungt).

## 17. MCP-tool gating

**Hvor:** Capabilities-rapporten har `mcp_servers: []` fordi MCP er
per-agent i cerebro. Persona's hook gater kun `claude.tool.*`.

**Det der mangler:** En agent's MCP-tools (fx `mcp__supabase__execute_sql`)
gates ikke. Hvis hook'en så `mcp__*` resource-kinds skulle den tjekke
mod sandbox-grants. Ingen kode der gør det endnu.

**Estimat:** 4t (kræver design af MCP resource-kind-konvention).

## 18. Cross-runtime tool reconciliation

**Hvor:** Hvis to runtimes (claude + codex) rapporterer hver deres
tool-liste, viser persona's UI dem pr. aktør men aggregerer dem ikke
til "tools på tværs af alle runtimes".

**Det der mangler:** En global `/tools`-side: "Bash bruges af 4
runtimes, gates af 2 sandboxes". Hjælper når man designer globale
politikker.

**Estimat:** 3t.

## 19. Re-sync når runtime opgraderes

**Hvor:** Daemon-restart trigger refresh (#3 fra del 1). Men hvis
claude CLI opgraderes uden daemon-restart, opdager daemonen ikke nye
tools.

**Det der mangler:** Heartbeat-loop tjekker om CLI-version ændrede sig,
re-rapporterer capabilities ved drift. Eller en
`/api/runtimes/{id}/refresh-capabilities`-endpoint operatøren kan
trigge.

**Estimat:** 2t.

---

## Kategori-oversigt for del 2

| # | Emne | Vægt | Estimat |
|---|---|---|---|
| 8 | Per-aktør grants UI | Skjult feature i UI'en | 3-4t |
| 9 | Token-listing + rotation | Security-blocker | 2-3t |
| 10 | Suspend/Delete knapper | Helt enkelt at lave | 1t |
| 11 | Søgning på /actors | Bliver ubrugelig over 20 aktører | 2t |
| 12 | Aktivitets-feed pr. sandbox | Bedre forståelse af blokeringer | 4t |
| 13 | Multica audit-log UI | Compliance / felsøgning | 3t |
| 14 | Sandbox-templates | Reducerer fejl ved design | 2t |
| 15 | Bulk actions | Operations-effektivitet | 2-3t |
| 16 | Per-tool resource_pattern | Den rigtige finkornighed | 6-8t |
| 17 | MCP-tool gating | Stort hul i sikkerhed | 4t |
| 18 | Cross-runtime tools-aggregat | Strategisk policy-overblik | 3t |
| 19 | Re-sync ved CLI-opgradering | Forhindrer drift | 2t |

**Total estimat for del 2:** ~34-39 timer.

---

## Management summary

Persona-cerebro-integrationen er funktionel for den primære brugssag —
daemon pusher tools til persona, runtime-cap håndhæves, e2e er grøn —
men der er to grupper af huller.

**Del 1 (7 punkter, ~10-13t)**: ting jeg sagde jeg byggede. Det største
er #1: persona's egentlige *scanner* ved stadig intet om cerebro's
runtimes på trods af at jeg påstod jeg lavede den integration.
Aktør-detalje-siden er kosmetisk OK, men dækningsrapporten i
`/gaps`-fanen ignorerer fortsat cerebro.

**Del 2 (12 punkter, ~34-39t)**: features der aldrig var i planen men
som mangler for at integrationen er komplet. De mest kritiske er #9
(token-rotation — security-blocker), #17 (MCP-tool-gating — stort
sikkerhedshul), og #16 (per-tool resource_pattern — det rigtige
finkornighedsniveau). #10 og #11 er små og hurtige UX-fix der gør UI'en
brugbar.

**Total**: ca. 19 punkter, ~44-52 timer for at få integrationen helt i
mål.

**Anbefaling**: lås del 1 #1, #3 og #4 ned før branchen merges
(merge-blockere). Del 2 kan splittes ud som separate features og
prioriteres efter hvad operatøren rent faktisk render ind i som
problem.
