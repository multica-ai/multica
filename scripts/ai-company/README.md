# AI Company scripts

Utilities for the `.ai-company/` operating system.

## install-harness.sh

Copy delivery harness into any git repository.

```bash
bash scripts/ai-company/install-harness.sh /path/to/target-repo
bash scripts/ai-company/install-harness.sh --dry-run ../my-site
```

Delegates to `.ai-company/harness/install.sh`.

## bootstrap-project.sh

One-shot: harness, `gh` labels, optional repo create/push, backlog → issues.

```bash
bash scripts/ai-company/bootstrap-project.sh ../music-game-sea \
  --repo your-org/music-game-sea \
  --create-repo --push \
  --sync-backlog --from TICKET-002 --to TICKET-007
```

## scaffold-landing.sh

Second product line — minimal Next.js landing + tool (no backend):

```bash
bash scripts/ai-company/scaffold-landing.sh ../landing-tool-a
bash scripts/ai-company/bootstrap-project.sh ../landing-tool-a \
  --repo your-org/landing-tool-a --create-repo --push --sync-backlog
```

## ensure-github-labels.sh

```bash
bash scripts/ai-company/ensure-github-labels.sh your-org/music-game-sea
```

## ceo-dashboard.sh

One-command portfolio summary:

```bash
bash scripts/ai-company/ceo-dashboard.sh
bash scripts/ai-company/ceo-dashboard.sh --dispatch --max-total 3
```

## ceo-workbench.sh

Local browser workbench — **唯一 CEO 入口**（工程队列 + 公司概览 + **内容线深链 hq.revoices.app**）:

```bash
bash scripts/ai-company/ceo-workbench.sh
# http://127.0.0.1:9477
```

Requires: `python3`, `gh`, logged-in `cursor-agent` for local dispatch.

**Feishu 建站 intake** 优先 POST `/api/site-factory`。常驻安装：

```bash
bash scripts/ai-company/ceo-workbench-service.sh install
bash scripts/ai-company/ceo-workbench-service.sh status
```

## site-factory.sh（飞书一句话建站）

```bash
bash scripts/ai-company/site-factory-verify.sh          # 16 项验收
bash scripts/ai-company/feishu-site-factory-smoke.sh    # 飞书等价路径 smoke
bash scripts/ai-company/feishu-site-factory-live-watch.sh 5  # Live 发消息后检查
bash scripts/ai-company/site-factory.sh --intake "做一个 XX 网站" --dry-run
```

烟测参考：`hello-cf-smoke`（Cloudflare Pages + 多 Agent 派单）。详见 `.ai-company/docs/15-feishu-site-factory.md`。

API includes `/api/multica-runtime` (daemon limits, per-agent concurrency, local CLI process breakdown). Same snapshot from CLI:

```bash
bash scripts/ai-company/multica-runtime-status.sh
bash scripts/ai-company/multica-runtime-status.sh --json
```

`ceo-dashboard.sh` prints the human snapshot at the bottom.

Local checkout paths are machine-specific — configure in `.ai-company/config/local.env`
(`MUSIC_SAAS_PATH` or `AI_REPO_PATH_<id>`), or let `resolve-repo-path.sh` auto-discover
under `~/Projects` / `~/Desktop`.

## portfolio-dispatch.sh

Multi-repo nightly dispatch from `project-registry.yaml` (run on company HQ / multica repo):

```bash
bash scripts/ai-company/portfolio-dispatch.sh --dry-run
bash scripts/ai-company/portfolio-dispatch.sh --max-total 5
```

**`dispatch_mode: multica`** (pilot: `meigen-replica`): assigns via `multica issue create` + daemon — tickets visible on `:3000/local/issues`. No CLI fallback (single queue). See `.ai-company/docs/34-multica-single-queue.md`.

**Legacy `dispatch_mode: local`** (default): `dispatch-cursor-agent-cli.sh` on CEO machine.

Enabled via `.github/workflows/portfolio-agent-dispatch.yml` (cron + manual).

**Content line** (`kind: content` in registry): triggers `content-delivery-dispatch.yml` on remote Hermes repo — no local path. See `.ai-company/docs/24-content-operations.md`.

**Product intel lounge** (好用版): `bash scripts/ai-company/setup-product-intel-lounge.sh` — agents, labels, autopilots. See `.ai-company/docs/35-product-intel-lounge.md`.

## install-content-harness.sh

Remote **Hermes media machine** — content repo (drafts, calendar, no CEO local clone):

```bash
bash scripts/ai-company/install-content-harness.sh /path/to/content-youtube-sea
```

Then register in `project-registry.yaml` with `kind: content`, `dispatch_mode: gha` or `remote-pull`.

## print-multica-autopilot-commands.sh

```bash
export MULTICA_DEV_AGENT_ID=<uuid>
bash scripts/ai-company/print-multica-autopilot-commands.sh
```

## scaffold-saas.sh

Third product line — SaaS shell; **payment paths human-only**:

```bash
bash scripts/ai-company/scaffold-saas.sh ../saas-stripe-mvp
```

## sync-backlog-to-issues.sh

Create GitHub Issues from a `backlog.md` file (e.g. `examples/music-game-sea/backlog.md`).

```bash
# Preview TICKET-001..003
bash scripts/ai-company/sync-backlog-to-issues.sh \
  --backlog .ai-company/examples/music-game-sea/backlog.md \
  --repo your-org/music-game-sea \
  --from TICKET-001 --to TICKET-003 \
  --dry-run

# Create all agent-safe tickets (after labels exist on repo)
bash scripts/ai-company/sync-backlog-to-issues.sh \
  --backlog ../music-game-sea/.delivery/music-game-sea/backlog.md \
  --repo your-org/music-game-sea
```

**Prerequisites:**

- `gh` CLI authenticated
- Repo labels: `agent-safe`, `agent-assisted`, `human-only` (+ runtime labels from `.delivery/README.md`)

Parses lines like:

```markdown
### TICKET-004 [agent-safe] 营销落地页 `/`
```

## sync-portfolio-backlogs.sh

Nightly refill: for each **active** registry project with
`.ai-company/examples/<delivery_slug>/backlog.md`, create missing `[TICKET-xxx]` issues.

```bash
bash scripts/ai-company/sync-portfolio-backlogs.sh --dry-run
bash scripts/ai-company/sync-portfolio-backlogs.sh
```

Enabled in `ceo-nightly.sh` when `CEO_SYNC_BACKLOG=1` (default).

Human-only lines (`PAY-001`, etc.) are only parsed if they use the same `### ID [grade] title` format.

## sync-company-norms.sh

Copy selected `.ai-company/` norm docs into each portfolio checkout at `.delivery/company-os/`.

```bash
bash scripts/ai-company/sync-company-norms.sh
bash scripts/ai-company/sync-company-norms.sh --id landing-tool-a --dry-run
bash scripts/ai-company/sync-company-norms.sh --harness --force-harness
```

Manifest: `.ai-company/config/company-os-sync-manifest.yaml`.  
Playbook: `.ai-company/docs/27-norm-sync.md`.

After sync, commit in each product repo: `.delivery/company-os/` + `.delivery/COMPANY-OS.md`.

## rollout-harness-tier0.sh / verify-harness-tier0.sh

Token-efficient **alwaysApply** cursor rules (pointers only — never full harness docs).

```bash
# One-shot: Vault sync + zbrain-session + company-harness + company-os
bash scripts/ai-company/rollout-harness-tier0.sh

# Verify all local HQ + portfolio + registry checkouts
bash scripts/ai-company/verify-harness-tier0.sh
```

Expected Tier-0 rules: see `.ai-company/harness/README.md` § Cursor Tier-0.

## record-harness-learning.sh / verify-harness-learnings.sh

Harness **experience feedback** (candidates queue → CEO weekly promote). Does not auto-edit norm docs.

```bash
bash scripts/ai-company/record-harness-learning.sh \
  --content "BLOCKED:INFRA cron PATH missing gh" \
  --suggest docs/23-local-agent-environment.md

bash scripts/ai-company/verify-harness-learnings.sh
```

Routing: `.ai-company/docs/31-harness-learnings-routing.md` · queue: `.ai-company/docs/system-evolution/harness-candidates.md`

## portfolio-commit-norms.sh

Batch `git add` + commit (+ optional push) for `CLAUDE.md` and company-os across portfolio:

```bash
bash scripts/ai-company/sync-company-norms.sh
bash scripts/ai-company/portfolio-commit-norms.sh --commit
bash scripts/ai-company/portfolio-commit-norms.sh --commit --push
```

## push-fork.sh

Push harness changes to **your fork** (`chenzh/multica`). Pushing to `origin` (`multica-ai/multica`) returns 403 unless you are a maintainer.

```bash
bash scripts/ai-company/push-fork.sh
# branch tracks fork/main after first push -u
```

## ceo-nightly.sh / ceo-daily-brief.sh

21:00 cron: reconcile → auto-merge → reconcile → background dispatch → brief + Feishu/Slack.

```bash
bash scripts/ai-company/install-nightly-cron.sh --install
bash scripts/ai-company/ceo-nightly.sh --no-dispatch      # brief only
bash scripts/ai-company/ceo-nightly.sh --sync-dispatch    # wait for agents before brief
bash scripts/ai-company/ceo-reconcile-queue.sh            # fix stale agent-* labels
```

Dispatch log (background): `~/.multica/ceo-nightly-dispatch.log`

## company-overview-snapshot.sh

JSON snapshot for daily brief (no HTTP server; add `--refresh-verify` to run verify-hands-off):

```bash
bash scripts/ai-company/company-overview-snapshot.sh | python3 -m json.tool
```

See `.ai-company/runbooks/nightly-ceo-brief.md`.

## ceo-feishu-approval.sh

Multica + Feishu dual CEO approval (BLOCKED / green PR). Syncs GitHub → Multica issue, pushes Feishu interactive cards, handles `/批` commands.

```bash
bash scripts/ai-company/setup-feishu-approval.sh --test
bash scripts/ai-company/ceo-feishu-approval-server.py   # callback :9478
bash scripts/ai-company/ceo-feishu-approval-service.sh install   # LaunchAgent 常开
bash scripts/ai-company/ceo-feishu-approval.sh list
bash scripts/ai-company/ceo-feishu-approval.sh sync
bash scripts/ai-company/ceo-feishu-approval.sh push
bash scripts/ai-company/ceo-feishu-approval.sh approve beatscape 42 说明
```

Set `CEO_FEISHU_APPROVAL_PUSH=1` in `local.env` to push cards after nightly brief. See `.ai-company/runbooks/feishu-approval.md`.
