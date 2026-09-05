# 33 — 自主迭代完整方案（硅谷 · 刘小排对表）

> **层级**：HQ 权威（不下发全文；产品仓链 `brief.md` + `.delivery/company-os/`）  
> **冻结**：2026-08-29  
> 配套：[02-operating-model.md](./02-operating-model.md) · [07-quality-gates.md](./07-quality-gates.md) · [21-label-state-machine.md](./21-label-state-machine.md) · [30-silicon-valley-doc-standards.md](./30-silicon-valley-doc-standards.md)  
> Runbook：[employee-autopilot.md](../runbooks/employee-autopilot.md) · [work-finder.md](../runbooks/work-finder.md) · [HANDS-OFF-COMPLETE.md](../HANDS-OFF-COMPLETE.md)

---

## 一句话

**自主迭代 = Work-Finder 造票 → Autopilot 派单 → Agent 开 PR → CI + merge-policy 自动合 → reconcile 清漂移；CEO 只投 brief 与接 BLOCKED。**

对齐硅谷：**Owner/DoD 写进系统 · 小批量可验证 · 分层自动化 · 背压不烧配额 · 人只接升级。**  
对齐刘小排：**闭环默认转 · 睡后系统转 · 评估器验真 · 置信度分流 · 人当指挥不当操作员。**

---

## 刘小排 ↔ 本仓落点

> 来源：公开分享中的 **Closed loop / 睡后运行 / 评估器 / 置信度** 等表述；本仓用工程化脚本落地，不依赖口头派活。  
> Runbook 入口：[employee-autopilot.md](../runbooks/employee-autopilot.md)（首行即「闭环默认转」）。

| 刘小排概念 | 含义（产品语言） | 本仓实现 | 验收 |
|------------|------------------|----------|------|
| **闭环默认转** | 默认 Closed loop：做 → 测 → 反馈 → 再迭代；拒绝 Open loop 碰运气 | 五段闭环 + reconcile 回卷；见下文架构图 | 无 PR/无 CI 结果不算「转完一圈」 |
| **睡后也在转** | 人不在电脑前，系统仍派活、交付、合入 | LaunchAgent（白天 30min）+ `ceo-nightly`（21:00） | `autopilot-launchagent.log` 有新轮次 |
| **评估器** | 每步有可判定成败的标准，不靠「感觉好了」 | `accept_cases.md`、Verifier、`make check`/Replica CI、`check-merge-eligible` | AC 与 CI 全是命令/exit code |
| **置信度分流** | 高置信 Agent 自行闭环；低置信才升级给人 | merge-policy **L1** auto-merge vs **L2** deny/BLOCKED；`06-task-grading` assisted 票 | deny 路径 merge 不告警（预期人工） |
| **岗位说明书** | 给 Agent「岗」而非零散 prompt | Issue body + `brief.md` + orchestrator kickoff；Work-Finder 按 backlog 造票 | 禁止聊天里口头派 `#8` |
| **信息装进系统** | 把判断所需上下文给 AI，而非每次重讲 | `.delivery/<slug>/`、Issue、policy、inventory/wont_do（复刻站） | kickoff 指向文件，不复制宪法全文 |
| **不盲烧配额** | 故障时停转，别空转烧 Token | `agent-blocked` 背压、`AUTOPILOT_MAX_BLOCKED`、空转飞书告警 | blocked≥阈值时不派 |
| **一人 + 多 Agent** | CEO 指挥，多产品线/多票并行 | `project-registry` 公平队列；`AUTOPILOT_MAX_CONCURRENT=2` | 人只投 brief / 接 BLOCKED |

**心法对照（一句话）**

```text
刘小排：把自己装进 AI，让闭环转起来。
本仓：把 brief/AC/policy 装进 repo + Issue，让 LaunchAgent/nightly 转起来。
```

**与硅谷表的关系**：刘小排偏 **产品运营闭环**（睡后、置信度、岗位）；硅谷表偏 **工程纪律**（lease、policy-as-code、branch protection）。两层互补，见下一节。

---

## 硅谷实践 ↔ 本仓落点

| 硅谷 / 大厂纪律 | 含义 | 本仓实现 | 验收 |
|-----------------|------|----------|------|
| **Single owner + DoD** | 每张票一个 implementer，完工标准可执行 | Issue AC + `accept_cases.md`；label `agent-done` 仅 CI 绿后 | AC 全是命令，无「looks good」 |
| **Small batch** | 一夜能 merge 的粒度 | Work-Finder `QUEUE_TARGET=3`；`max_nightly_tickets` | 单 PR diff &lt; 可审范围 |
| **Branch protection + auto-merge** | 绿 CI 才进 main | `agent-delivery-gate.yml` + `merge-policy.json` | `check-merge-eligible.sh` 绿 → `gh pr merge --auto` |
| **Trunk-based / short-lived branches** | `cursor-issue-*` 短命分支 | `dispatch-cursor-agent-cli.sh` worktree | head 前缀匹配 policy |
| **Idempotent workers** | 重复调度不双跑 | 派单租约 lock + `issue_dispatch_active` | 假 `agent-running` reconcile 清 |
| **Lease / TTL** | 僵尸不占槽 | `DISPATCH_LEASE_SECONDS`（默认 2h） | `cleanup_stale_local_dispatches` |
| **Backpressure** | 故障时停止入队 | `AUTOPILOT_MAX_BLOCKED`（默认 5） | blocked 多时不派 |
| **Reconcile loop** | 标签漂移自动修 | `ceo-reconcile-queue.sh` | 无 live dispatch 摘 `agent-running` |
| **On-call 升级矩阵** | 只有 P0/BLOCKED 叫人 | 飞书仅 BLOCKED / 空转 / CI 红 | deny-path merge **不告警** |
| **Machine credentials** | CI/调度不用人肉 session | macOS **LaunchAgent**（GUI 会话）或 `CURSOR_API_KEY` | `cursor-agent -p` 在 nohup 内成功 |
| **Portfolio fairness** | 多产品线抢资源 | `project-registry.yaml` priority + `max_total` | 高 priority 先吃槽 |
| **Policy as code** | 谁能 auto-merge 写进 repo | `.delivery/config/merge-policy.json` | deny 胜 allow |
| **No chat truth** | 决策在 Issue/PR | orchestrator kickoff 指向文件 | 禁止口头派活 |
| **Bounded scope + kill switch** | 自动化只消费已承诺 backlog；一键停 lane | `brief`/`wont_do`/`paused`；见 **[有界脱手](#有界脱手什么时候会停不是永动机)** | 种子吃完 idle，不自动开新品 |

---

## 端到端闭环（五段）

```text
┌─────────────┐   ┌──────────────┐   ┌─────────────┐   ┌──────────────┐   ┌─────────────┐
│ 1. 造票      │ → │ 2. 排队       │ → │ 3. 派单      │ → │ 4. 交付       │ → │ 5. 合入      │
│ Work-Finder │   │ GitHub Labels │   │ Autopilot   │   │ cursor-agent │   │ gate+merge  │
│ backlog sync│   │ agent-safe    │   │ portfolio   │   │ PR + CI      │   │ main        │
└─────────────┘   └──────────────┘   └─────────────┘   └──────────────┘   └─────────────┘
       ↑                                      │                                    │
       └──────── reconcile 清 BLOCKED/假 running ←──────────────────────────────────┘
```

### 1 — 造票（Work-Finder）

- **触发**：QUEUE &lt; `QUEUE_TARGET`（默认 3）；周末/工作日 cron（见 [work-finder.md](../runbooks/work-finder.md)）
- **范围**：仅 `project-registry` 非 paused + 已有 `examples/<slug>/backlog.md`
- **禁止**：新站、改 merge-policy、密钥、支付、`human-only`
- **产出**：`sync-portfolio-backlogs.sh --skip-existing` → GitHub Issues（`agent-safe`）

### 2 — 排队（Labels + Registry）

| Label | 调度器行为 |
|-------|------------|
| `agent-safe` | 可派 |
| `agent-running` | 跳过（有 live dispatch 时保留） |
| `agent-blocked` | 跳过；计入背压 |
| `agent-done` | 跳过；有 open PR 时保持 |

台账：`templates/project-registry.yaml`（priority、`paused`、`max_nightly_tickets`）。

### 3 — 派单（Autopilot + Portfolio）

**值班经理**：`autopilot-dispatch.sh`

每轮顺序：

1. `cleanup_stale_local_dispatches` — 杀僵尸 / 过期 lock  
2. `ceo-reconcile-queue.sh` — 标签漂移、PR 冲突 → blocked  
3. `ceo-auto-merge.sh`（`CEO_AUTO_MERGE=1`）— 白天也合绿 PR  
4. `portfolio-dispatch.sh --local --max-total N` — 按 priority 派 `agent-safe`

**并发**：`AUTOPILOT_MAX_CONCURRENT=2`（本机 CLI 真并发槽，非 label 数）。

**pick 规则**：`pick_next_issue` 排除 `agent-blocked` / `agent-running` / `agent-done`。

### 4 — 交付（Agent + PR）

- `dispatch-cursor-agent-cli.sh <issue>` → worktree `cursor-issue-<N>`  
- Prompt：`build-prompt.sh` + orchestrator kickoff  
- 成功：PR + `agent-done`；失败：`agent-blocked` + log 路径  
- Auth 失败检测：log 内 `Authentication required` → blocked（可 reconcile 重试）

### 5 — 合入（分层 Merge）

| 层 | 谁合 | 条件 |
|----|------|------|
| **L1 自动** | GHA `agent-delivery-gate` 或 `ceo-auto-merge` | CI 绿 + `merge_eligible=true` + `branchNamePrefix` 匹配 |
| **L2 人工** | CEO | deny 路径（workflows、auth、migrations）、冲突、assisted 票 |
| **L3 禁止** | — | 密钥扫描红、incident |

`check-merge-eligible.sh`：deny 胜 allow；`requireLabels` 校验关联 issue。

---

## 双时钟（昼夜不停转）

| 时钟 | 调度器 | 周期 | 脚本 |
|------|--------|------|------|
| **白天** | macOS **LaunchAgent**（推荐） | 30 min + RunAtLoad | `autopilot-launchagent-service.sh install` |
| **白天备用** | cron | 工作日 :15；周末 */30 | `install-autopilot-cron.sh --install` |
| **夜间** | cron 21:00 | 每日 | `install-nightly-cron.sh --install` → `ceo-nightly.sh` |

安静时段：**23:00–06:00**（Asia/Shanghai）Autopilot 不派单（脚本内强制）。

`ceo-nightly.sh` 顺序：reconcile → auto-merge → reconcile → sync-backlog → dispatch → 飞书日报。

---

## 一次性落地（CEO 机器）

在 **multica HQ** 执行：

```bash
# 0. 验收基线
bash scripts/ai-company/verify-hands-off.sh

# 1. 本机凭证（二选一）
cursor-agent login && cursor-agent status
# 或 local.env: export CURSOR_API_KEY=...   # cron/LaunchAgent 兜底

# 2. 路径与密钥
cp .ai-company/config/local.env.example .ai-company/config/local.env
# 填 GITHUB_ORG、AI_REPO_PATH_*、CEO_AUTO_MERGE=1

# 3. 代理：Clash 没开时不要留死 proxy.env（或让 source-local-env 自动 unset）

# 4. 调度器（macOS）
bash scripts/ai-company/autopilot-launchagent-service.sh install
bash scripts/ai-company/install-nightly-cron.sh --install

# 5. 通知（可选）
bash scripts/ai-company/setup-feishu-bot-notify.sh

# 6. 产品仓规范下发
bash scripts/ai-company/sync-company-norms.sh
```

### 每个产品仓必备

| 工件 | 路径 |
|------|------|
| Harness | `.cursor/agents/`、`.delivery/` |
| Merge policy | `.delivery/config/merge-policy.json` |
| Gate workflow | `.github/workflows/agent-delivery-gate.yml` |
| Dispatch 脚本 | `scripts/agent-delivery/check-merge-eligible.sh` |
| Brief + AC | `.delivery/<slug>/brief.md`、`accept_cases.md` |
| 种子 backlog | HQ `examples/<slug>/backlog.md` → sync |

**案例**：`meigen-replica` — `visual-replica` stack，`branchNamePrefix: cursor-issue`。

---

## 控制面硬化（2026-08-29 已落）

| 机制 | 脚本 | 解决的问题 |
|------|------|------------|
| 派单租约 | `agent-queue.sh` `write_dispatch_lock` | 僵尸占并发槽 |
| 假 running 清理 | `reconcile_stale_running_labels` | 进程死标签还在 |
| Auth blocked 重试 | `reconcile_auth_blocked_retries` | 鉴权恢复后可再派 |
| 背压 | `AUTOPILOT_MAX_BLOCKED` | blocked 堆积仍盲派 |
| Glob 修复 | `check-merge-eligible.sh`（Python path match） | deny 误杀全文件 |
| `-R` 作用域 | `gh pr/issue view -R` | 本地 merge 查错 PR |
| 分支前缀 | `ceo-auto-merge` 读 `branchNamePrefix` | `cursor-issue-*` 不走 policy |
| 白天 merge | `autopilot-dispatch` 调 `ceo-auto-merge` | 不必等 21:00 |

---

## 有界脱手：什么时候会停（不是永动机）

**无人值守 ≠ 无止境执行。** 脱手的是 **日常派单、合 PR、清标签**；**边界**由 CEO 在 brief / backlog / registry 里事先画好。机器在盒子里闭环转，盒子外不擅自扩 scope。

### 三层「停」

| 层级 | 停什么 | 典型条件 | 会不会自己再醒 |
|------|--------|----------|----------------|
| **一轮调度** | 本轮回合不派、不合 | QUEUE=0、满并发、背压、安静时段 | ✅ 下轮 LaunchAgent/nightly 仍醒 |
| **造票** | Work-Finder 不续 backlog | QUEUE≥target、种子耗尽、quiet hours | ✅ 队列薄了可能再造 |
| **产品线** | 整条 lane 停工 | `paused: true`、Issue 全关、brief 目标达成且不再续票 | ❌ 直到 CEO 续 brief / 取消 paused |

### 硬边界（系统内置）

```text
安静时段     23:00–06:00 不派单
队列目标     Work-Finder 仅当 TOTAL_QUEUE < 3 造票
单轮上限     max_total / max_nightly_tickets / CEO_AUTO_MERGE_MAX
并发上限     AUTOPILOT_MAX_CONCURRENT = 2
背压         BLOCKED ≥ 5 → 暂停派单
单票重试     verify 耗尽 → agent-blocked → 等人
禁区         wont_do、merge-policy deny、human-only 分级
```

### 软边界（CEO 定义，机器遵守）

| 边界文件 | 回答的问题 |
|----------|------------|
| `brief.md` | 这个产品要做到哪、**不做什么** |
| `wont_do.md` | 找活工 / Agent **永远不能碰**什么 |
| `accept_cases.md` | 每张票怎么算「完」 |
| `backlog.md` | 已知票清单；**吃完不自动无限增生**（除非 Work-Finder 在 QUEUE 薄时按启发式补小票） |
| `project-registry.yaml` | `paused`、`priority`、`max_nightly_tickets` |

**「业务做完」**不是 cron 里一个 `exit 0`，而是：**brief 范围内的票 merge 完 + CEO 不再续 backlog / 设 paused**。例如 meigen：复刻站 visual gate + 种子 TICKET-008 做完后，若不续 TICKET-009，系统自然 idle——**不是坏了，是盒子里没活了**。

### 脱手 vs 撒手

| | 脱手（设计目标） | 撒手（要避免） |
|--|------------------|----------------|
| 边界 | brief + wont_do + policy 已写 | 没 brief 就指望 Agent 自己找方向 |
| 异常 | BLOCKED → 飞书；人处理后再转 | blocked 堆积仍盲派、烧 Token |
| 结束 | 目标达成后 paused 或不再续票 | 以为它会「自己知道何时停」 |

### 硅谷怎么做（有界自动化，不是永动机）

大厂无人值守 / 高自动化团队也不会让系统 **无限自生成目标**。常见做法是 **多层刹车 + 显式 scope**，与本仓一一对应：

| 硅谷实践 | 他们在停什么 | 本仓落点 |
|----------|--------------|----------|
| **WIP limit（Kanban）** | 同时进行的工作有上限，防止全员过载 | `AUTOPILOT_MAX_CONCURRENT=2`、`QUEUE_TARGET=3` |
| **Sprint / OKR / Roadmap 边界** | 机器/团队只消化 **已承诺 backlog**，不擅自开新 OKR | `brief.md` + `backlog.md`；Work-Finder 不新开产品线 |
| **Small batch + trunk** | 小 PR、短命分支，降低单次自动化爆炸半径 | `cursor-issue-*` + merge-policy **allow 白名单** |
| **Autonomy tiers（L1/L2/L3）** | 高自主仅低风险路径；支付/权限/workflow 必须人批 | L1 gate auto-merge；L2 deny/BLOCKED；`06-task-grading` |
| **Circuit breaker / backpressure** | 下游失败率升时 **停止入队**，避免 retry storm | `AUTOPILOT_MAX_BLOCKED`、满并发 `action=busy` |
| **Error budget（SRE）** | SLO 烧尽则 **冻结发布**，先修可靠性 | CI 不绿不合；连续空转飞书告警 |
| **Kill switch** | 一键停某服务/某区域发布 | `project-registry` **`paused: true`**；`autoMergeEnabled: false` |
| **Change advisory / launch review** | 改基础设施、auth、DB 迁移要走审查 | merge-policy **deny**（workflows、migrations、auth） |
| **PagerDuty 升级矩阵** | 只有 **P0 / 需人判断** 的才叫醒 on-call | 飞书：BLOCKED / 空转 / CI 红；**deny merge 不告警** |
| **FinOps / quota cap** | 月度 Token、CI 分钟、并发 Agent 封顶 | `max_nightly_tickets`、`CEO_AUTO_MERGE_MAX`；`lib/budget-guard.sh` + `pause_autopilot_on_exceed`（[10](./10-cost-and-budget.md)） |
| **Definition of Done（Epic 级）** | Epic 完 = 验收清单全绿，**不是**「永远有下一个 story」 | `accept_cases.md` + Issue 全关；CEO 决定是否续 epic |
| **Out of scope（RFC）** | 设计阶段写清 **Non-goals**，防止 scope creep | `wont_do.md`、Issue out-of-scope 段 |

**硅谷共识（一句话）**

```text
Automation runs inside a committed backlog, with WIP limits, autonomy tiers,
and circuit breakers — humans own scope and kill switches, not every PR.
```

与本仓「有界脱手」同构：**调度器可以永远醒着（LaunchAgent），但只在 CEO 划定的盒子里消费 backlog；盒子空了或 paused，就 idle，而不是去发明新商业。**

成本护栏详见 [10-cost-and-budget.md](./10-cost-and-budget.md)（`pause_autopilot_on_exceed` 已由 `budget-guard.sh` 在 `autopilot-dispatch` 入口执行，构成 **FinOps 层**硬停）。

---

## CEO 边界（真睡觉）

| CEO 做 | 机器做 |
|--------|--------|
| 投 brief / 新品线（飞书 site-factory） | 造票、派单、开 PR、跑 CI |
| 回 BLOCKED / 审批 deny 路径 merge | reconcile、auto-merge L1 |
| 月/季调 priority、paused | 公平队列、背压、空转告警 |
| **不**日常 `dispatch --force` | **不**口头派活 |

---

## 可观测性

| 看什么 | 路径 |
|--------|------|
| LaunchAgent 心跳 | `~/.multica/autopilot-launchagent.log` |
| 每轮决策 | `~/.multica/autopilot-logs/autopilot-*.log` |
| 派单明细 | `~/.multica/autopilot-logs/portfolio-bg-*.log` |
| Agent 日志 | `<product>/.delivery/.agent-runs/issue-*.log` |
| 队列仪表盘 | `bash scripts/ai-company/ceo-dashboard.sh` |
| 夜间总览 | `~/.multica/ceo-nightly.log` |

**健康信号**：`cursor-agent -p` 进程存在 · 新 PR 出现 · `agent-safe` 队列下降 · 无连续空转告警。

---

## 故障自愈矩阵

| 症状 | 根因 | 自愈动作 |
|------|------|----------|
| QUEUE 有票不派 | `agent-blocked` 双标签 | reconcile / auth-retry |
| 假满员 | 僵尸 dispatch / **cleanup 误杀**（`cursor-agent -p` 匹配不到 `index.js -p --worktree`） | 修 `agent-queue.sh` pgrep；reconcile running |
| `Authentication required` in log | cron 无 session | **LaunchAgent** 或 `CURSOR_API_KEY` |
| `gh` EOF | 死代理 7897 | 关 `proxy.env` 或开 Clash |
| CI 绿不合 | glob / 无 linked issue | 修 `check-merge-eligible`；PR 关 issue |
| PR conflict | main 前进 | reconcile → `agent-blocked`；下轮 agent 或 CEO |
| Work-Finder 不造票 | QUEUE ≥ target | 正常背压；消化后再造 |
| 空转告警 | 派了 QUEUE 不降 | 查 auth、worktree、BLOCKED |

---

## 验收（Definition of Done — 系统级）

```bash
bash scripts/ai-company/verify-hands-off.sh          # 应全绿
bash scripts/agent-delivery/check-merge-eligible.test.sh
launchctl print gui/$(id -u)/com.multica.ai-company-autopilot  # LaunchAgent 在册
bash scripts/ai-company/ceo-dashboard.sh --json     # QUEUE>0 时能派
```

**端到端烟测**（观察，不手派）：

1. Work-Finder dry-run 能造票 → sync 出 `agent-safe` issue  
2. 等 LaunchAgent 一轮 → `.agent-runs/` 有新 log  
3. PR 开 → CI 绿 → gate 或 ceo-auto-merge 合入 main  
4. issue 标签清为 done / PR merged  

---

## 与 Multica 产品 Autopilot 的关系

| | Portfolio（本方案） | Multica Autopilot（可选） |
|--|---------------------|---------------------------|
| 调度 | HQ 本机 LaunchAgent + nightly | `multica autopilot` cron/webhook |
| Worker | `cursor-agent` CLI | Multica runtime agent |
| 历史 | 文件日志 + GitHub | Multica 看板 run 历史 |
| 推荐 | **默认主路径** | 辅助看板 |

见 [14-multica-autopilot-portfolio.md](./14-multica-autopilot-portfolio.md)。

---

## 相关文档

- [35-product-intel-lounge.md](./35-product-intel-lounge.md) — 产品情报站（好用版 · 热点 → 口令 → Issue）
- [06-task-grading.md](./06-task-grading.md) — agent-safe vs human-only  
- [18-definition-of-done.md](./18-definition-of-done.md)  
- [22-git-and-remotes.md](./22-git-and-remotes.md) — fork 纪律  
- [23-local-agent-environment.md](./23-local-agent-environment.md)  
- [runbooks/blocked-triage.md](../runbooks/blocked-triage.md)  
- [runbooks/visual-replica-gate.md](../runbooks/visual-replica-gate.md)
