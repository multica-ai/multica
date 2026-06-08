# Deploy — firtal-cerebro

## Miljøer

| Miljø | URL | Gren | Platform |
|---|---|---|---|
| Staging | `https://Sara.firtal.com` | `main` | Mac mini (sara.local, launchd) |
| Produktion | `https://Multica.firtal.com` | `production` | Sliplane (Docker-containers) |
| Lokal dev | `http://localhost:3000` | din branch | — |

- **`main`** deployer løbende til staging (`Sara.firtal.com`) via webhook på sara-serveren.
- **`production`** deployer til produktion (`Multica.firtal.com`) via Sliplane — kræver godkendt release-issue.

## Hvad der trigger et deploy

**Merge til `main` deployer til staging, ikke produktion.** Produktion opdateres først når `main` flettes ind på grenen `production`, og den fletning kræver en `approve`-kommentar på et release-issue i Multicas "Deployments"-projekt.

**Stående regel:** Merge til `main` så snart CI er grøn — vent ikke. Staging opdateres automatisk, og deploy-godkendelse til produktion kræver alligevel et separat skridt (release-issue).

Det automatiske flow (`auto-deploy-trigger` autopilot) opretter release-issuet for dig — du skal kun godkende. Sliplane bygger og deployer automatisk når `production` modtager et push.

## Deploy-flowet — trin for trin

1. PR flettes til `main` (sker løbende, hele dagen). Staging (`Sara.firtal.com`) opdateres automatisk.
2. `auto-deploy-trigger`-autopiloten fyrer (GitHub-webhook ind i Multica, med en planlagt fallback der hver X. minut sammenligner `main` mod `production`). Den slår appen op i `registry.firtal.com`, kører `release-review`-tjeklisten på diffen `main..production`, og opretter ÉT release-issue i Deployments-projektet (`ecb4fb83-0995-48a5-97d2-3adce73aa800`) pr. ventende udgivelses-vindue. Næste push på samme vindue opdaterer SAMME issue (idempotent på repo + main-head-sha) — 5 PR'er på 10 min = 1 godkendelse, ikke 5.
3. App-ejeren (eller Jesper) kommenterer `approve` + tagger agenten på release-issuet. Agenten fletter den stående `main → production`-PR via GitHub-API'en.
4. Push til `origin/production` trigger Sliplane til at bygge nye Docker-containers og deploye til `Multica.firtal.com`.

## Hvem godkender?

| Ændringstype | Reviewer | Godkender merge |
|---|---|---|
| Lille (< 3 filer, ingen brugervendt impact) | — | Dig selv |
| Mellemstor (feature, UI, API) | Tine (QA) | Du efter Tines godkendelse |
| Høj risiko (auth, prod-data, betaling, breaking) | Tine (QA) | Sara — vent på eksplicit go |

Godkendelse sker som en `approve`-kommentar på release-issuet i Multica (Deployments-projektet) — IKKE som en knap eller direkte merge i GitHub-UI.

**Aldrig deploy fredag** uden Saras eksplicitte godkendelse.

## Verificering efter deploy

**Staging (`main` → `Sara.firtal.com`):**
1. Åbn `https://Sara.firtal.com` og tjek at appen loader
2. Tjek `.deploy/logs/deploy-latest.log` på sara-serveren for fejl

**Produktion (`production` → `Multica.firtal.com`):**
1. Åbn `https://Multica.firtal.com` og tjek at appen loader
2. Tjek Sliplane's deploy-log for fejl
3. Rollback via Sliplane hvis containeren fejler — eller revert commit på `production`-grenen

## Launchd-jobs (staging — på sara-serveren)

Disse jobs kører staging-miljøet (`Sara.firtal.com`) fra `main`-grenen:

- `com.multica.frontend` — Next.js web-app
- `com.multica.backend` — Go API-server
- `com.multica.daemon` — Multica daemon
- `com.multica.webhook` — GitHub webhook-receiver (lytter på push til `main`)

Manuel genstart:
```bash
launchctl kickstart -k gui/$(id -u)/com.multica.frontend
```

## Manuel fallback (staging)

Hvis webhook-listeneren ikke fyrer eller serveren har været offline:

```bash
ssh sara@<runner-host>
bash ~/code/firtal-cerebro/.deploy/deploy.sh
```

Det henter `origin/main` (staging) og kører hele deploy-flowet manuelt.
