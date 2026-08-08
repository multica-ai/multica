# Platform Extension Squad Runtime Live Acceptance

## Scope

This record covers the dedicated live acceptance of a Platform Extension imported into Multica and executed by a Desktop-owned `platform-agent-cli` Runtime in Mock mode. It verifies the authenticated product API, native Multica resources, client rendering, runtime allocation, sidecar materialization, and the real Daemon task lifecycle without calling an external model.

## Fixture Contract

Source fixture: `testdata/extensions/task11-live-squad.source.json`

| Resource | Required value |
| --- | --- |
| Extension | `task11-live-squad@1.0.0` |
| Agents | `acceptance-leader`, `acceptance-reviewer` |
| Leader | `acceptance-leader` |
| Skills | `evidence-check`, `result-review` |
| Flow Command | `delegate.flow` |
| Runtime Command | `summarize` |
| Bindings | 2 Agents x 2 Skills = 4 |
| Runtime | one Online `platform-agent-cli` Runtime shared by both Agents |
| Execution output | Extension key/version, Leader source key, `skills=2`, `commands=1`, and non-empty `input=` containing the current Issue ID |

## Acceptance Environment

| Component | Live value |
| --- | --- |
| Workspace | `zx-test` (`219a6616-a8fc-4ba0-9921-f60d31bff1e5`) |
| Daemon profile | `desktop-localhost-8080` |
| Daemon ownership | `MULTICA_LAUNCHED_BY=desktop` |
| Daemon ID | `019fdb30-9f0f-7df6-b0bc-0cce48609d62` |
| Execution mode | `PLATFORM_AGENT_MODE=mock` |
| CLI version | `platform-agent-cli 0.2.0` |
| Multica revision | `cb1eea9f2533359f90885985b15f108ca728c3af` |
| CLI revision | `e8c021c8f0551c78bfd00401df27ae731ec47f4d` |
| CLI SHA-256 | `06662929044f84a5ac7962451a6c3bf6b38dcfc51ef32814d1f3b646954c3a8a` |
| Server SHA-256 | `1193bcc2670e4af34771a466a37b82d8c1dba8d4b9f5c59e5c49360004c9e97f` |
| CLI/Daemon SHA-256 | `16693519d33231547d74ecacd93b9d2a55cc50435a001346b8b762cadee1b14c` |
| CLI path | `/Users/zxx/Documents/技术学习/platform-agent-cli/bin/platform-agent-cli` |
| Server | real local source server on `localhost:8080` with PostgreSQL migrations applied |
| Client | current Multica Web renderer on `localhost:3000`, using the Desktop profile token |

All three final binaries were rebuilt from `git archive` exports of the revisions above. The Multica archive excluded the shared worktree's uncommitted plan and Task 11 evidence files, and the CLI archive came from a clean worktree.

## First Live Run

The authenticated `POST /api/extensions/import` request returned `201` and `idempotent=false`. The harness then verified the canonical Bundle and native database state before assigning a real Issue to the imported Leader.

| Resource | Live identifier |
| --- | --- |
| Release | `a30c7d1c-9313-4ec4-83b9-384ef3b65200` |
| Digest | `sha256:0cc0346c912e9b9940fbc808be6da7186c90679c97157df02382e740247531aa` |
| Runtime | `54d53675-3513-43e1-ad71-d4f5b250bc0c` |
| Squad | `f47ba2a5-a077-485e-8622-1e1d2ab5dfe2` |
| Leader Agent | `3c1f3b1d-f514-4048-91b7-b73ae8743719` |
| Member Agent | `c55d56ca-8e6c-439a-9fd7-ebcd228efbf8` |
| Evidence Skill | `3b39b74d-e61a-4e1d-bed4-4fbf93ca44e3` |
| Review Skill | `20ef7e49-e0f8-46b0-9eb5-4a4ad02541ff` |
| Issue | `c1d79c7f-940d-40ef-845d-e1fc90e9bb50` |
| Task | `bd6839ba-b6c4-4124-b755-04e5568017b0` |

Verified before execution:

- exactly one immutable Release and one Squad;
- exactly two native Agents and two native Skills;
- exactly four `agent_skill` bindings;
- Leader and member roles match the source fixture;
- both Agents are fixed to Runtime `54d53675-3513-43e1-ad71-d4f5b250bc0c`;
- `delegate.flow` is present in Squad Instructions and excluded from the runtime sidecar;
- `summarize` is present once in each Agent runtime sidecar and excluded from Agent and Squad instructions;
- the support file `references/checklist.md` is persisted;
- runtime metadata reports `platform-agent-cli 0.2.0` and `launched_by=desktop`.

The first Task reached terminal status `failed` with no persisted messages:

```text
platform-agent-cli thread/start failed: thread/start: RUNTIME_CONTEXT_INVALID: RUNTIME_CONTEXT_INVALID: skill root must be a real directory (code=-32003)
```

The real Task work directory contained a regular Daemon ownership control file at `.agent_context/skills/.multica-sidecar-owner`. It also contained nine Multica operational Skills in addition to the two Extension-bound Skills. These observations identified two integration defects that must be corrected before final acceptance:

1. the CLI must ignore only valid regular Daemon ownership control files while preserving fail-closed handling for other unexpected entries;
2. Platform Agent claims must materialize exactly the Agent-bound Extension Skills, not Multica operational Skills.

Focused tests for both corrections passed before the final rebuild. This document does not mark the execution acceptance as passed until a new Task completes through the rebuilt binaries.

### Intermediate functional diagnostic

After the ownership-file correction, Task `b3105ba4-7ea8-44cc-8093-d7369b47dbbc` completed against a stale pre-filter server but correctly failed the harness because it reported `skills=11`. After restarting the server from Multica commit `b2a186c`, Task `d8fa7983-0f30-4eea-95df-42ae9aee3778` also completed with `skills=11`. The live Daemon negotiates Skill references, and that path still used `LoadAgentSkillBundles`, which appended nine built-in Skills to the two bound Extension Skills. This diagnostic is not an acceptance PASS; it requires the negotiated Skill-reference path to preserve the same Platform exact-bound contract.

### Intermediate functional proof

The negotiated Skill-reference correction was rebuilt from exact Multica commit `cb1eea9`; the CLI was rebuilt from exact commit `e8c021c`. No uncommitted shared-worktree changes were included.

| Artifact | SHA-256 |
| --- | --- |
| `platform-agent-cli` | `06662929044f84a5ac7962451a6c3bf6b38dcfc51ef32814d1f3b646954c3a8a` |
| Multica server | `7c95acc69ceb9bee8c5b4af9b9d27f3375c02d4bc7698dd369c0798857c020e2` |
| Multica CLI/Daemon | `b259d585f4174bda54be70eb4a231538ffaaf3c24bd617497dcb5894dfb30bc3` |

The same Extension import returned `200` with `idempotent=true`. Issue `ffb5cd3c-9088-4f4f-9da4-b16513ea3399` (`ZXT-5`) created Task `1e736e60-3203-4745-88e6-ed4f2a4e3864`, which completed on the imported Leader and Runtime. One output message was persisted. The complete Mock output was:

```text
extension=task11-live-squad@1.0.0 release=a30c7d1c-9313-4ec4-83b9-384ef3b65200 digest=sha256:0cc0346c912e9b9940fbc808be6da7186c90679c97157df02382e740247531aa agent=acceptance-leader skills=2 commands=1 input=You are running as a local coding agent for a Multica workspace.

Your assigned issue ID is: ffb5cd3c-9088-4f4f-9da4-b16513ea3399

**Turn mode: Ownership.** Follow the Ownership-mode block in your runtime workflow file for this turn; the Reply-mode rules do not apply.

Start by running `multica issue get ffb5cd3c-9088-4f4f-9da4-b16513ea3399 --output json` to understand your task, then complete it.
For comment history, follow the rule in your runtime workflow file (assignment-triggered tasks treat the read as mandatory). Scan the threads first with `multica issue comment list ffb5cd3c-9088-4f4f-9da4-b16513ea3399 --roots-only --summary --compact --output json`, then expand only what matters with `--thread <thread-id> --tail 30`. For `--since` incremental polling, pagination, and folding, see `multica issue comment list --help`.
```

All functional harness assertions passed in this run. It is retained as intermediate evidence; the formal acceptance below used separately rebuilt binaries and a new Issue and Task.

## Client Verification

The current client route `http://localhost:3000/zx-test/extensions` was opened in headless installed Chrome with the existing Desktop profile token. The page rendered:

- Extension `task11-live-squad`, version `1.0.0`, and the persisted digest;
- Runtime `Platform Agent CLI`;
- Squad `Task11 Live Squad v1.0.0`;
- Leader and Reviewer Agents;
- Evidence Check and Result Review Skills;
- the Import Extension action.

The page issued authenticated `GET /api/extensions` and `GET /api/extensions/a30c7d1c-9313-4ec4-83b9-384ef3b65200` requests, both returning `200`. Evidence screenshot: `/private/tmp/task11-extensions-client.png`.

The page was refreshed again after the formal final Task `994130af-a39a-4345-a087-523f73220ac5` completed. Release, Runtime, Squad, both Agents, and both Skills remained visible. The refreshed network evidence was:

```text
GET /api/extensions                                               200
GET /api/extensions/a30c7d1c-9313-4ec4-83b9-384ef3b65200       200
```

The client was started without reinstalling dependencies:

```bash
cd apps/web
./node_modules/.bin/next dev --webpack --port 3000
```

The browser used `http://localhost:3000`, matching the Next.js development origin. `http://127.0.0.1:3000` is intentionally rejected for development HMR unless `allowedDevOrigins` is widened and was not used as acceptance evidence.

The Electron window and its existing login state were not driven through GUI automation. The acceptance instead uses the same renderer package, the same authenticated product API, the real Desktop profile, and a Daemon explicitly marked `launched_by=desktop`.

## Commands

The final binaries were built from isolated exports of the pinned revisions. The equivalent build commands inside those exports are:

```bash
GOCACHE=/private/tmp/task11-final-cli-gocache CGO_ENABLED=0 \
  /Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go build -trimpath \
  -ldflags '-s -w -X main.version=0.2.0' \
  -o /absolute/path/to/platform-agent-cli ./cmd/platform-agent-cli

GOCACHE=/private/tmp/task11-final-multica-gocache CGO_ENABLED=0 \
  /Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go build -trimpath \
  -ldflags '-s -w -X main.version=task11-final -X main.commit=cb1eea9f2533359f90885985b15f108ca728c3af' \
  -o /absolute/path/to/multica-server ./cmd/server

GOCACHE=/private/tmp/task11-final-multica-gocache CGO_ENABLED=0 \
  /Users/zxx/Documents/技术学习/.tools/go1.26.1/bin/go build -trimpath \
  -ldflags '-s -w -X main.version=task11-final -X main.commit=cb1eea9f2533359f90885985b15f108ca728c3af' \
  -o /absolute/path/to/multica ./cmd/multica
```

The server was started in terminal 1:

```bash
cd /path/to/multica/server
set -a
source ../.env
set +a
/absolute/path/to/multica-server
```

The Desktop-owned Mock Daemon was restarted from terminal 2:

```bash
MULTICA_LAUNCHED_BY=desktop \
MULTICA_PLATFORM_AGENT_CLI_DESKTOP_BUNDLED_ONLY=1 \
MULTICA_PLATFORM_AGENT_CLI_PATH=/absolute/path/to/platform-agent-cli \
PLATFORM_AGENT_MODE=mock \
  /absolute/path/to/multica daemon restart \
    --profile desktop-localhost-8080 --no-auto-update --no-auto-reload
```

Run the harness assertions:

```bash
node --test scripts/platform-extension-runtime-acceptance.test.mjs
```

Run the first fresh import and execution:

```bash
set -a
source .env
set +a
TASK11_EXPECT_FRESH=1 TASK11_TIMEOUT_MS=90000 \
  node scripts/platform-extension-runtime-acceptance.mjs
```

The post-fix rerun requires the existing mapping and creates a new Leader Issue and Task:

```bash
set -a
source .env
set +a
MULTICA_PLATFORM_AGENT_CLI_PATH=/absolute/path/to/platform-agent-cli \
TASK11_EXPECT_IDEMPOTENT=1 TASK11_TIMEOUT_MS=90000 \
  node scripts/platform-extension-runtime-acceptance.mjs
```

The harness requires this import to return `200` with `idempotent=true`.

`TASK11-LIVE-INPUT` remains in the Issue title so acceptance records are easy to locate. Multica's real Issue execution lifecycle sends the Runtime an Ownership brief containing the assigned Issue ID; it does not copy the Issue title into the `app-server` turn. The runtime assertion therefore verifies a non-empty `input=` value containing the newly created Issue ID, and verifies the same Issue ID in the persisted Task message.

## Final Live Rerun

**PASS.** The formal rerun completed at `2026-08-08T13:52:52.771Z` using the exact revisions and SHA-256 values in Acceptance Environment. Re-import returned `200` with `idempotent=true`, preserving the original Release, Runtime, Squad, Agents, Skills, and four bindings.

| Result | Live value |
| --- | --- |
| Issue | `0622d65e-dfa3-4105-b901-89dc09aafa18` (`ZXT-6`) |
| Task | `994130af-a39a-4345-a087-523f73220ac5` |
| Task status | `completed` |
| Assigned Agent | `3c1f3b1d-f514-4048-91b7-b73ae8743719` (`acceptance-leader`) |
| Runtime | `54d53675-3513-43e1-ad71-d4f5b250bc0c` |
| Persisted messages | `1` |

The complete persisted Mock output was:

```text
extension=task11-live-squad@1.0.0 release=a30c7d1c-9313-4ec4-83b9-384ef3b65200 digest=sha256:0cc0346c912e9b9940fbc808be6da7186c90679c97157df02382e740247531aa agent=acceptance-leader skills=2 commands=1 input=You are running as a local coding agent for a Multica workspace.

Your assigned issue ID is: 0622d65e-dfa3-4105-b901-89dc09aafa18

**Turn mode: Ownership.** Follow the Ownership-mode block in your runtime workflow file for this turn; the Reply-mode rules do not apply.

Start by running `multica issue get 0622d65e-dfa3-4105-b901-89dc09aafa18 --output json` to understand your task, then complete it.
For comment history, follow the rule in your runtime workflow file (assignment-triggered tasks treat the read as mandatory). Scan the threads first with `multica issue comment list 0622d65e-dfa3-4105-b901-89dc09aafa18 --roots-only --summary --compact --output json`, then expand only what matters with `--thread <thread-id> --tail 30`. For `--since` incremental polling, pagination, and folding, see `multica issue comment list --help`.
```

The harness also reasserted the one Release/one Squad mapping, two Agents, two Skills, four bindings, leader/member roles, Flow Command placement in Squad Instructions, ordinary Command placement in both runtime sidecars, the persisted Skill support file, both Agents' shared Runtime, Desktop daemon ownership, CLI version, current Issue ID in runtime input, and the same Issue ID in the persisted message. The final client refresh and its two authenticated `200` responses are recorded in Client Verification.
