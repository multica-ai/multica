# Deploy — firtal-cerebro

## Hvad der trigger et deploy

**Merge til `main` deployer IKKE direkte.** Live-deployet sker først når `main` flettes ind på grenen `production`, og den fletning kræver en `approve`-kommentar på et release-issue i Multicas "Deployments"-projekt.

Det automatiske flow (`auto-deploy-trigger` autopilot) opretter release-issuet for dig — du skal kun godkende. GitHub-webhook'en på `sara.tailbde0.ts.net` deployer fra `production` (ikke `main`).

## Deploy-flowet — trin for trin

1. PR flettes til `main` (sker løbende, hele dagen).
2. `auto-deploy-trigger`-autopiloten fyrer (GitHub-webhook ind i Multica, med en planlagt fallback der hver X. minut sammenligner `main` mod `production`). Den slår appen op i `registry.firtal.com`, kører `release-review`-tjeklisten på diffen `main..production`, og opretter ÉT release-issue i Deployments-projektet (`ecb4fb83-0995-48a5-97d2-3adce73aa800`) pr. ventende udgivelses-vindue. Næste push på samme vindue opdaterer SAMME issue (idempotent på repo + main-head-sha) — 5 PR'er på 10 min = 1 godkendelse, ikke 5.
3. App-ejeren (eller Jesper) kommenterer `approve` + tagger agenten på release-issuet. Agenten fletter den stående `main → production`-PR via GitHub-API'en.
4. Push til `origin/production` rammer webhook-listeneren på sara (`com.multica.webhook` på port 9000), som kører `.deploy/deploy.sh`.
5. `.deploy/deploy.sh`:
   1. `git fetch origin production` + `git reset --hard origin/production` — exit hvis ingen nye commits.
   2. Bygger backend og kører migrationer.
   3. Genstarter `com.multica.backend` og `com.multica.daemon` straks efter migrationer, så gamle binaries ikke serverer trafik mod nyt schema.
   4. Bygger Next.js-frontend (`apps/web`) til `.next.new/` (sletter `.next` først pga. Next.js 16 stale client-reference-manifest).
   5. Atomisk swap: `.next` → `.next.old`, `.next.new` → `.next`.
   6. Genstarter `com.multica.frontend`.
   7. Smoke-test: HTTP 200 på `/` og én chunk-route.
   8. Fejler smoke-test → automatisk rollback til `.next.old`.

Concurrent merges serialiseres via lock i `.deploy/logs/deploy.lock`. Se `JEH-628` for baggrunden (parallelle builds der SIGTERM'ede hinanden).

## Hvem godkender?

| Ændringstype | Reviewer | Godkender merge |
|---|---|---|
| Lille (< 3 filer, ingen brugervendt impact) | — | Dig selv |
| Mellemstor (feature, UI, API) | Tine (QA) | Du efter Tines godkendelse |
| Høj risiko (auth, prod-data, betaling, breaking) | Tine (QA) | Sara — vent på eksplicit go |

Godkendelse sker som en `approve`-kommentar på release-issuet i Multica (Deployments-projektet) — IKKE som en knap eller direkte merge i GitHub-UI.

**Aldrig deploy fredag** uden Saras eksplicitte godkendelse.

## Verificering efter deploy

1. Åbn `https://sara.tailbde0.ts.net` og tjek at appen loader
2. Tjek `.deploy/logs/deploy-latest.log` på sara-serveren for fejl
3. Rollback sker automatisk ved smoke-test-fejl — men verificér manuelt hvis du er i tvivl

## Miljøer

| Miljø | URL | Gren |
|---|---|---|
| Produktion | `https://sara.tailbde0.ts.net` | `production` |
| Arbejdsgren (ikke live) | — | `main` |
| Lokal dev | `http://localhost:3000` | din branch |

Der er ikke et staging-miljø. Test lokalt eller på en feature-branch inden merge til `main`.

## Launchd-jobs (på sara-serveren)

- `com.multica.frontend` — Next.js web-app
- `com.multica.backend` — Go API-server
- `com.multica.daemon` — Multica daemon
- `com.multica.webhook` — GitHub webhook-receiver (lytter på push til `production`)

Genstartede automatisk af deploy-scriptet. Manuel genstart:
```bash
launchctl kickstart -k gui/$(id -u)/com.multica.frontend
```

## Manuel fallback

Hvis autopiloten ikke fyrer eller runneren har været offline:

```bash
ssh sara@<runner-host>
bash ~/code/firtal-cerebro/.deploy/deploy.sh
```

Det henter `origin/production` og kører hele deploy-flowet manuelt.
