# Deploy — firtal-cerebro

## Hvad der trigger et deploy

**Merge til `main` = automatisk deploy.**

GitHub-webhook'en på `sara.tailbde0.ts.net` kalder `.deploy/deploy.sh`
umiddelbart efter merge. Der er ingen manuel deploy-kommando.

## Deploy-processen trin for trin

1. `.deploy/deploy.sh` henter seneste `main` fra GitHub
2. Bygger Next.js-frontend (`apps/web`) til `.next.new/`
3. Atomisk swap: `.next` → `.next.old`, `.next.new` → `.next`
4. Genstarter launchd-jobs: `com.multica.frontend`, `com.multica.backend`,
   `com.multica.daemon`
5. Smoke-test: HTTP 200 på `/` og én chunk-route
6. Fejler smoke-test → automatisk rollback til `.next.old`

Concurrent merges serialiseres via lock i `.deploy/logs/deploy.lock`.
Se `JEH-628` for baggrunden (parallelle builds der SIGTERM'ede hinanden).

## Hvem godkender?

| Ændringstype | Reviewer | Godkender merge |
|---|---|---|
| Lille (< 3 filer, ingen brugervendt impact) | — | Dig selv |
| Mellemstor (feature, UI, API) | Tine (QA) | Du efter Tines godkendelse |
| Høj risiko (auth, prod-data, betaling, breaking) | Tine (QA) | Sara — vent på eksplicit go |

**Aldrig deploy fredag** uden Saras eksplicitte godkendelse.

## Verificering efter deploy

1. Åbn `https://sara.tailbde0.ts.net` og tjek at appen loader
2. Tjek `.deploy/logs/deploy-latest.log` på sara-serveren for fejl
3. Rollback sker automatisk ved smoke-test-fejl — men verificér manuelt
   hvis du er i tvivl

## Miljøer

| Miljø | URL | Branch |
|---|---|---|
| Produktion | `https://sara.tailbde0.ts.net` | `main` |
| Lokal dev | `http://localhost:3000` | din branch |

Der er ikke et staging-miljø. Test lokalt eller på en feature-branch
inden merge.

## Launchd-jobs (på sara-serveren)

- `com.multica.frontend` — Next.js web-app
- `com.multica.backend` — Go API-server
- `com.multica.daemon` — Multica daemon
- `com.multica.webhook` — GitHub webhook-receiver

Genstartede automatisk af deploy-scriptet. Manuel genstart:
```bash
launchctl kickstart -k gui/$(id -u)/com.multica.frontend
```
