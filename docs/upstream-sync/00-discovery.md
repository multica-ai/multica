# Step 0 — Discovery findings

Verifikation af forudsætninger og opdagelse af scope vi måtte have overset.

## Findings

### F1 — Migration-værktøjet tracker ved fuldt filnavn, ikke nummer

**Verificeret i:** `server/cmd/migrate/main.go`

Migration-version er det fulde basenavn (uden `.up.sql`/`.down.sql`). To filer med samme nummer-prefix men forskellige navne tælles som **to forskellige migrationer**:

```go
// version = "050_project_color"
// version = "050_agent_model"  ← upstream — får ny række i schema_migrations
```

**Konsekvens:**
- Vores eksisterende `050_project_color`, `052_project_repo`, `055_user_profile`, `056_attachment_chat_message`, `060_runtime_sandbox_enabled` kolliderer ikke med upstream's `050_agent_model`, `052_add_cloud_waitlist_to_users` osv.
- Begge kører bare når man kører `migrate up`
- Filer sorteres alfabetisk → `050_agent_model` kører før `050_project_color` — **deterministisk men kan være forkert rækkefølge** hvis de afhænger af hinanden (begge på samme tabel)

**Hvad vi gør:**
- Vi behøver ikke renumbere eksisterende migrationer (de er allerede kørt i prod)
- For NYE cerebro-migrationer: brug `9NNN_cerebro_*.sql` namespace (vi gør det allerede fra 9001)
- Per-merge eval: kør `make migrate-up` på en frisk DB og verificér at alle migrationer kører grønt

### F2 — 7 cerebro-pakker er allerede isoleret

**Verificeret:** Disse mapper eksisterer kun i vores fork (upstream har dem ikke):

```
packages/core/artifacts/
packages/core/attachments/
packages/core/notifications/
packages/views/artifacts/
packages/views/attachments/
packages/views/notifications/
packages/views/members/
```

**Konsekvens:**
- Disse 7 pakker er ikke i konflikt-listen overhovedet (med undtagelse af `members/index.ts`)
- De skal **rename** til `cerebro-*` prefiks, ikke flyttes til ny placering
- Renamearbejde: ~7 mapper × 30 filer total = trivielle imports-opdateringer

**Reduceret arbejde for L1:** Phase 1's "ekstraktion" bliver delvist en rename-operation, ikke nyt arbejde.

### F3 — Settings-page understøtter allerede tab-extension via prop

**Verificeret:** `packages/views/settings/components/settings-page.tsx` har `extraAccountTabs?: ExtraSettingsTab[]` prop som desktop-app bruger til at injicere DaemonSettingsTab.

**Konsekvens for feature flag tab:**
- Vi kan bruge SAMME mekanisme — ingen patch i settings-page nødvendig
- Cerebro registrerer en `ExtraSettingsTab` med feature flag-toggles
- I app-roden tilføjer vi cerebro-tabs til `extraAccountTabs` arrayet
- **Settings-page falder dermed ud af konflikt-listen** (den var L2 i auditen, bliver nu L1)

### F4 — Kun ét feature flag i koden i dag

**Verificeret:** `MULTICA_ENABLE_SANDBOX` er den eneste env-baserede toggle. Der findes intet generelt feature flag-system.

**Konsekvens:**
- Vi designer feature flag-systemet fra bunden i cerebro-pakke
- Architecture: Zustand-store i `packages/cerebro-feature-flags/` der persisterer til localStorage + server (workspace-scoped)
- Default-on for alle cerebro-features; brugeren kan toggle hver feature off via Settings → Feature Flags tab
- Server-side: tilføj `cerebro_feature_flags` tabel der gemmer per-bruger overrides

### F5 — Åbne feature-branches der vil divergere mens refactor kører

**Verificeret:** Følgende lokale branches har commits ud over main:

```
feat/access-combined-flow-and-keylock
fix/claude-empty-output-diagnostics-JEH-405
fix/project-access-response-field
phase-B-persona-integration  (på pause per memory)
work/15e8b958-rebase
```

Plus origin-branches inkl. `feat/per-runtime-sandbox-JEH-418` og flere.

**Konsekvens:**
- Hvis disse branches landes i main MENS refactor kører → refactor må re-baselineæres
- Disciplin-announcement skal sendes til team før Phase 1 starter
- Eksisterende åbne PR'er bør lande FØR vi starter, ellers efter Phase 1

**Mitigation:** Phase 0 indeholder team-koordinering + freeze-vindue mens refactor lander.

### F6 — Untrackede platform-spec docs i `docs/specs/`

**Verificeret:** I main-checkouten findes utrackede filer:

```
docs/specs/firtal-cerebro-platform-design.md
docs/specs/firtal-cerebro-platform-progress.txt
docs/specs/firtal-cerebro-platform-requirements.md
docs/specs/firtal-cerebro-platform-research.md
docs/specs/firtal-cerebro-platform-tasks.md
docs/specs/firtal-cerebro-platform.json
docs/specs/firtal-cerebro-platform.md
```

**Konsekvens:**
- Disse er brugerens arbejdsdokumenter omkring den større platform-vision
- De er **ikke en del af upstream-sync-scopen** men kan referenceres
- Filenavne bruger "firtal-cerebro" — overvej rename til `cerebro-platform-*` for konsistens (men det er en separat beslutning)

### F7 — Test-infrastruktur stærk nok til eval-suite

**Verificeret:**
- Playwright e2e/ har 7 specs (759 linjer total): auth, navigation, issues, comments, notifications, projects-access, settings
- `playwright.config.ts` eksisterer og er konfigureret
- Vitest til packages/core, packages/views, apps/web
- Go test for backend
- `make check` kører alt sammen

**Konsekvens:**
- Eval-suite kan udvides oven på eksisterende infrastruktur — ingen nye værktøjer
- Visual regression understøttes natively af Playwright (`toHaveScreenshot`)
- Alle nye E2E-specs i `e2e/` følger eksisterende mønster

### F8 — Idempotente migrationer er allerede fix'd

**Verificeret:** Recent commit `d6b9d181 fix(migrations): make every migration idempotent (JEH-356)` — alle migrationer er allerede idempotente.

**Konsekvens:**
- Kør samme migration to gange er sikkert
- Vi kan re-køre `make migrate-up` flere gange under refactor uden at korrumpere DB

### F9 — Sandbox er platform-specifik

**Verificeret:** `MULTICA_ENABLE_SANDBOX` referer til macOS sandbox-exec via `prepareCommand` i `server/pkg/agent/`. Linux/Windows har ikke samme sandbox.

**Konsekvens:**
- Sandbox-tests skal markeres `test.skip(process.platform !== 'darwin')`
- CI på Linux skal ikke fejle på sandbox-tests

### F10 — Pre-existing untrackede filer i main

**Status:**
- `CLAUDE-DAMAGE-AUDIT.md`, `CLAUDE-RECOVERY-PLAN.md` — utrackede planer fra tidligere session
- `server/internal/realtime/audience_test.go` — ny test, utracket
- `.gitignore` modified

**Konsekvens:**
- Beslut om disse filer skal commit'es eller slettes før refactor starter
- Ikke en del af refactor-scopen, men skal cleanup'es for ren udgangspunkt

## Opsummering: scope-justeringer

Baseret på discovery skal følgende justeres i de oprindelige plan-dokumenter:

1. **01-audit.md** — opdater L1-listen: tilføj 7 allerede-isolerede pakker der bare skal renames (ikke nye flytninger)
2. **01-audit.md** — `settings-page.tsx` flytter fra L2 til L1 (bruger eksisterende extraAccountTabs prop)
3. **03-decision.md** — tilføj Phase -1 preflight med 8 verifikationspunkter
4. **03-decision.md** — Phase 0 inkluderer team-koordinering + freeze-vindue
5. **03-decision.md** — feature flag-system designes som første artefakt i Phase 0
6. **03-decision.md** — go/no-go-kriterier pr fase
7. **04-evals.md** — Phase -1 eval = de 8 preflight-eksperimenter
8. **04-evals.md** — sandbox-tests markeres platform-specifikke
9. **05-risks.md** — ny fil, formaliserer risikoregister
10. **06-session-prompt.md** — ny fil, self-contained kickoff til frisk session
