# Step 2 — Assessment

Vurdering af auditen i `01-audit.md`. Spørgsmålet er: er fordelingen sund nok til at investere i refactor-vejen, eller har vi underestimeret koblingen?

## Fordeling af 107 kode-konflikter

| Lag | Filer | % | Konflikt-cost efter refactor |
|---|---|---|---|
| L0 — Auto-resolve | 36 | 34% | 0 (script) |
| L1 — Additive (flyt) | 14 | 13% | 0 (i cerebro-zonen) |
| L2 — Composition (wrap) | 15 | 14% | 0 (vi ejer ikke filen) |
| L3 — Marked patch | 42 | 39% | ~3-8 filer pr sync, ≤5 linjer hver |

**Sundhedstegn:** 61% af konflikterne forsvinder helt (L0+L1+L2). Resterende 39% er reduceret til <5 linjer pr fil i gennemsnit.

**Bekymringstegn:** L3 er 42 filer — flere end mit oprindelige mål om "≤15 patches". Det skyldes at server-kode er bredt koblet (29 af de 42 er server-side). Hver server-fil har dog kun 3-5 linjers patch.

## Kritisk vurdering pr lag

### L0 er ren teknisk

36 filer som ikke kræver kreativ tankegang. Risiko: lav. `make sqlc` regenererer; `pnpm install` regenererer; docs er ligegyldige (per beslutning). Eneste lommedybde: hvis upstream renamer en kolonne i `queries/*.sql` der ramler i en af vores migrationer — sjældent, manuel håndtering når det sker.

### L1 er stort set risikofrit

14 filer der bare flyttes til en ny sti. Risiko: lav.

**Eneste reelle udfordring:** `packages/views/inbox/components/inbox-page.tsx`. Vi har ændret +1198 -108 linjer. Det er for stor en ændring til wrap. Beslutningen er: **vi ejer cerebro's inbox 100%, upstream's inbox-evolution er ikke interessant for os**. Det er en bevidst arkitekturbeslutning der skal valideres med stakeholder før vi committer:

- **Pro replacement:** ingen merge-konflikter på inbox-kode, vi kontrollerer UX (folders, archive, multi-select)
- **Con replacement:** upstream's bug-fixes og perf-forbedringer i deres inbox kommer ikke automatisk til os — vi skal manuelt cherry-picke hvis de fixer noget kritisk

### L2 er hvor risikoen ligger

15 filer der konverteres til wrappers. Risiko: **medium-høj**.

**Reelle udfordringer:**

1. **Wrap-pattern fejler hvis upstream ændrer komponentens props/API.** Når upstream tilføjer en required prop, eller fjerner en vi bruger, bryder vores wrapper. Det fanges af TypeScript/`make check` ved merge-tid, ikke ved konflikt-tid. Bedre end silent breakage, men stadig arbejde.

2. **Ikke alle modifikationer er rent compose-bare.** Eksempler hvor det er svært:
   - `comment-card.tsx`: vi har tilføjet `flattenReplies()` helper og ændret import-struktur. Helperen kan flyttes til cerebro-package, men ændringer i hvordan upstream-komponenten itererer over replies er svære at kompose
   - `agents-page.tsx`: upstream har removed -187 linjer + added +826. Vores wrap af base-komponenten kan blive dyrere at vedligeholde end inline-modifikationer
   - `chat-message-list.tsx`: vi har -50 linjer (real surgery) hvor vi har ændret hvordan upstream-komponenten viser beskeder. Wrap fungerer ikke for "ændre intern logik"

   **Risiko-mitigation:** for filer hvor wrap er svært, fall-back til L3 patches. Konkret estimat: 3-5 af L2's 15 filer kan ende som L3 i praksis.

3. **Composition-overhead:** hver wrapped komponent betyder ekstra render-cost (typisk negligibelt) og en ekstra abstraktionslag for udviklere. Det handler om at vægte vedligeholdelses-cost vs konflikt-cost.

### L3 er bedst-case kombination af pragmatik + disciplin

42 filer med små inline-patches. Risiko: lav-medium.

**Hvad gør det robust:**
- `CEREBRO-PATCH(<navn>):` markører gør patches grep-bare og tæl-bare
- Patch-script auto-validerer at hver markør stadig findes efter merge
- Hver patch ≤5 linjer betyder konflikter er trivielle at løse (ofte auto-resolvable af git med `merge.conflictStyle = zdiff3`)

**Hvad kan gå galt:**
- Upstream fjerner kontekst omkring en patch (fx renamer en metode vi patcher) → manuel re-applikation, men med markører er det <10 min arbejde pr patch
- Vi glemmer at markere en ny patch → konfliktfladen vokser stille. Mitigation: lint-regel der kræver `CEREBRO-PATCH:` markør på enhver modifikation af upstream-fil

## Token-cost-estimat (revideret)

| Aktivitet | Filer | Tokens/fil | Sub-total |
|---|---|---|---|
| L0 setup script | — | — | 80k |
| L1 ekstraktion til cerebro-zonen | 14 | 50k | 700k |
| L1 inbox-page replacement (special case) | 1 (men stort) | 200k | 200k |
| L2 wrapper-design + implementation | 15 | 60k | 900k |
| L3 marker + dokumentér patches | 42 | 8k | 336k |
| Eval-suite (se 04-evals.md) | — | — | 600k |
| Verification + fix regressions | — | — | 400k |
| **Engangs total** | | | **~3,2M tokens** |

**Pr fremtidig sync efter refactor:** ~150-300k tokens (auto-resolve er gratis, patch-validation 30k, manuel resolution af 3-8 patches × 20k, `make check` 50-100k).

vs naiv merge nu: ~1,8M pr sync. **Breakeven efter ~2-3 sync.**

## Hot-spots der bekymrer mig

### inbox-page.tsx (+1198 linjer)
For stort til wrap. Vi ejer det. Stakeholder-beslutning kræves: er vi OK med at upstream's inbox-arbejde ikke flyder til os automatisk? Anbefaling: ja — vi har bygget en anden inbox-UX og vil ikke have den ændret af upstream.

### agents-page.tsx (vores +131 -83 vs upstream +826 -187)
Upstream har ombygget komponenten markant. Vores wrap-strategi her er mere kompliceret end normalt. Mitigation: gør den til L3 i stedet. 30 linjer markeret patch i stedet for 50 linjers wrapper-kode.

### app-sidebar.tsx (9 commits, dyb integration)
Vores ændringer er spredt: nye nav-items, mobile auto-close, notifications-icon, chat_session pin-ikoner, Documents-indgang. Kan ikke meningsfuldt wrappes. Bedst: L3 med ~15 linjers patch (lidt over budget).

### packages/core/api/client.ts (+382 linjer, 20 commits)
Ikke en wrap-kandidat. Vores plan: patch-mount cerebro-api sub-client (3-5 linjer i client.ts), implementering ligger i `packages/cerebro-api/`. Det fjerner 380 af de 382 linjer fra konfliktfladen.

## Hvad der KAN gå galt med planen

1. **Upstream introducerer en arkitektur-ændring der bryder vores composition-model.**
   Eksempel: upstream skifter fra props til context for sidebar-konfiguration. Vores L2 wrappers brydes alle på én gang.
   Sandsynlighed: lav, sker ~1×/år. Cost når det sker: ~500k tokens at re-arkitektere wrappers.

2. **Patches driver: nye cerebro-features fortsætter med at modificere upstream-filer i stedet for at lande i cerebro-zonen.**
   Mitigation: PR-template + lint-regel der kræver `CEREBRO-PATCH:` markør og afviser ændringer i upstream-filer uden eksplicit godkendelse.

3. **Eval-suite er ikke dækkende nok til at fange regressioner.**
   Mitigation: se `04-evals.md` — vi tager baseline-snapshots før refactor og kører identiske scenarier efter.

4. **Wrap-overhead viser sig at koste mere end forudset.**
   Mitigation: efter Phase 2 (de første 5 wrappers) — re-evaluer om wrap-strategien er sund eller om vi skal flytte mere til L3.

## Anbefaling

**Gå videre med refactor-planen** med følgende justeringer ift. min oprindelige skitse:

- L3-budget hæves fra "≤15 patches" til "≤45 patches, ≤200 linjer total". Mere realistisk ift. data.
- L2-strategi for `agents-page.tsx`, `chat-message-list.tsx`, `app-sidebar.tsx` flyttes til L3 — wrap er for dyrt for disse.
- Inbox-replacement (L1 special) kræver eksplicit go-ahead.
- Hver fase ender med eval-gate (se `04-evals.md`).

Engangs-cost ~3,2M tokens, breakeven efter 2-3 sync, derefter ~250k pr sync. Med upstream's velocity (~50 commits/måned, 4-6 sync/år rimelig cadence) tjener investeringen sig ind inden for 6-9 måneder.

Næste step: `03-decision.md`.
