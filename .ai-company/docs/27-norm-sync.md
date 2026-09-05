# 27 — 规范同步（三层管道 + 操作手册）

> **问题**：`.ai-company/` 宪法在 multica HQ，产品仓 Agent 默认读不到。  
> **解法**：认清三条同步管道 + `sync-company-norms.sh` 把选定规范复制到各产品 `.delivery/company-os/`。

---

## 三层规范，三种同步

```text
层 1  SecondBrain Harness          层 2  company-harness           层 3  Company OS 文档
      (编码习惯 / Vault)                 (派单 / Agent / CI)              (宪法 / runbook)

权威  Vault HARNESS/*.md           multica .delivery/ + agents      multica .ai-company/
命令  sync-all-harness.sh          install-harness.sh [--force]     sync-company-norms.sh
落到  .cursor/rules/               .delivery/ · agents · GHA        .delivery/company-os/
      docs/VAULT-HARNESS.md        scripts/agent-delivery/
范围  registry.json 里所有         project-registry 各产品 checkout  project-registry 各产品 checkout
      有 .secondbrain 的外接仓
自动  手动一条命令                 仅安装时复制；--force 才覆盖      手动一条命令（本脚本）
```

**不要混用：**

| 你想更新… | 用这条命令 | 不要指望… |
|-----------|------------|-----------|
| Vault 全局 / profile / 项目 harness | `sync-all-harness.sh` | 不会动 `.ai-company/` |
| Agent prompt、workflow、merge-policy | `install-harness.sh --force <repo>` | 不会复制 docs/18 等宪法 |
| 任务分级、DoD、BLOCKED 流程 | `sync-company-norms.sh` | 不会改 SecondBrain 快照 |

---

## 层 1 — SecondBrain（外接仓 Cursor 规则）

```bash
bash "$HOME/Documents/SecondBrain/10-SYSTEM/scripts/sync-all-harness.sh" --skip-pull
```

**写入各仓：**

- `.cursor/rules/vault-harness.mdc`
- `docs/VAULT-HARNESS.md`（`global` + `profile` + `projects/{slug}` 快照）

**前提：** 项目在 `SecondBrain/10-SYSTEM/HARNESS/registry.json` 且根目录有 `.secondbrain`。

**口令：** `同步项目规范到全部外接仓库`

multica 的 `SESSION.md`：改 Vault harness 后跑此命令并复验 Doctor。

---

## 层 2 — company-harness（执行件）

```bash
bash scripts/ai-company/install-harness.sh /path/to/product-repo
bash scripts/ai-company/install-harness.sh --force /path/to/product-repo   # 覆盖已有
```

**复制：** `.delivery/` 骨架、`.cursor/agents/`、`.cursor/rules/company-harness.mdc`、GHA workflows、`scripts/agent-delivery/`、Issue 模板。

**不复制：** `.ai-company/docs/` 全文（仅生成 `COMPANY-OS.md` 指针）。

### Cursor 省 token（全对话仍遵循 harness）

| Tier | 注入方式 | 放什么 | 大约体量 |
|------|----------|--------|----------|
| **0 触发器** | `.cursor/rules/*.mdc` + `alwaysApply: true` | 阅读顺序指针；**禁止**贴宪法全文 | 每条 ≤25 行 |
| **1 按需** | Agent 开干前 `Read` | `docs/VAULT-HARNESS.md`、`.delivery/company-os/`、`CLAUDE.md` | 全文在文件里 |
| **2 任务** | Issue / kickoff | AC、out of scope | 单票范围 |

**multica HQ 固定 Tier 0（4 条）：** `vault-harness` · `zbrain-session` · `company-harness` · `code-index`  
**产品仓固定 Tier 0（有 `.secondbrain` 时 3 条）：** `vault-harness` · `zbrain-session` · `company-harness`（`install-harness.sh` 安装）

**User Rules（Cursor 设置）** 只放个人偏好（语言、commit 习惯），**不要**重复 harness 正文——否则每条对话双倍扣 token。

刷新 Tier 0：`sync-all-harness.sh` · `install-harness.sh --force` · `rollout-harness-tier0.sh` · `verify-harness-tier0.sh`。

**经验回流**（不写进 Rules）：[31-harness-learnings-routing.md](./31-harness-learnings-routing.md) · `record-harness-learning.sh` · `verify-harness-learnings.sh` · 队列 [system-evolution/harness-candidates.md](./system-evolution/harness-candidates.md)。

批量：

```bash
bash scripts/ai-company/bootstrap-all-products.sh
```

≥3 项目长期方案：独立 `company-harness` repo + submodule（见 `harness/README.md`）。

---

## 层 3 — Company OS 规范（本管道）

### 清单（manifest）

编辑 [config/company-os-sync-manifest.yaml](../config/company-os-sync-manifest.yaml)，列出要下发到产品仓的相对路径（相对 `.ai-company/`）。

新增规范文档（如未来的 `docs/18-definition-of-done.md`）后：

1. 写入 multica `.ai-company/docs/`
2. **追加到 manifest**
3. 跑同步命令
4. 各产品仓 `git commit` + `push`

### 同步命令

```bash
# 全 portfolio（跳过 paused、跳过无本机 path 的项）
bash scripts/ai-company/sync-company-norms.sh

# 单个项目
bash scripts/ai-company/sync-company-norms.sh --id landing-tool-a

# 预览
bash scripts/ai-company/sync-company-norms.sh --dry-run

# 同时刷新 harness（agents + workflows）
bash scripts/ai-company/sync-company-norms.sh --harness --force-harness

# 含 paused 项目
bash scripts/ai-company/sync-company-norms.sh --include-paused
```

**写入产品仓：**

```text
<product-repo>/
  .delivery/
    COMPANY-OS.md          # 指针（本脚本每次重写）
    company-os/
      README.md            # 快照索引 + multica commit + 时间戳
      docs/06-task-grading.md
      runbooks/blocked-triage.md
      …                    # manifest 所列文件
```

**本机 path 解析：** 与派单相同 — `local.env` 的 `AI_REPO_PATH_<id>`、`repo-paths.local.yaml`、或 `AI_COMPANY_REPO_SEARCH` 自动发现。

### 同步后必做（各产品仓）

```bash
# 批量 stage + commit（本机各 checkout）
bash scripts/ai-company/portfolio-commit-norms.sh --commit
bash scripts/ai-company/portfolio-commit-norms.sh --commit --push   # 并 push origin

# 或单仓手动：
cd /path/to/product-repo
git add CLAUDE.md .delivery/company-os .delivery/COMPANY-OS.md
git status
git commit -m "chore: sync company-os norms from multica"
git push origin
```

HQ 仓（multica）本身 **不需要** 跑 `sync-company-norms` — 权威源已在 `.ai-company/`。

---

## CEO 规范变更标准流程

```text
1. 在 multica 编辑 .ai-company/docs/ 或 runbooks/
2. 若影响 Agent 行为 prompt → 考虑是否改 .cursor/agents/ → install-harness --force
3. 若影响 Vault 编码习惯 → 改 SecondBrain HARNESS → sync-all-harness.sh
4. 若影响任务分级 / DoD / BLOCKED → 更新 manifest（若新文件）→ sync-company-norms.sh
5. multica: git commit + push-fork.sh
6. 各产品: commit 同步后的 .delivery/company-os/
7. 可选: bash scripts/ai-company/verify-hands-off.sh
```

---

## Agent 读规范顺序（产品仓内）

编排层应在 kickoff prompt 中要求 Worker 按序阅读：

1. 项目 `CLAUDE.md`（若有）
2. `.delivery/<slug>/brief.md` + `accept_cases.md`
3. `.delivery/company-os/docs/06-task-grading.md`
4. `.delivery/company-os/docs/07-quality-gates.md`
5. `.delivery/company-os/runbooks/blocked-triage.md`
6. `.delivery/README.md` + `.cursor/agents/*.md`

快照过期时以 HQ multica `.ai-company/` 为准；产品仓副本用于 **离线 / 单仓 clone 派单**。

---

## 与指挥舱的关系

[17-ceo-cockpit.md](./17-ceo-cockpit.md) 的「规范入口」链 HQ 文档；  
产品仓 Agent 读 `.delivery/company-os/`。两者内容应对齐 manifest。

指挥舱后续可加：**上次 `sync-company-norms` 时间 / multica sha**（读各仓 `company-os/README.md` 或 HQ 日志）。

---

## 故障排查

| 现象 | 处理 |
|------|------|
| `skip: no local checkout` | 在 `local.env` 设 `AI_REPO_PATH_<id>=/path` |
| `no .delivery/` | 先 `install-harness.sh` 或 `--harness` |
| manifest 里文件 missing | 路径写错或未 commit 到 multica |
| 产品 Agent 仍像不知道规范 | 检查 kickoff 是否链 `company-os/`；重跑 sync |
| SecondBrain 与 Company OS 冲突 | 分层：Vault=编码习惯；company-os=交付宪法 |

---

## 相关文档

- [28-norm-layers.md](./28-norm-layers.md) — 通用 vs 项目 vs 任务放哪
- [29-harness-layout.md](./29-harness-layout.md) — 按类型索引（docs / examples / harness / multica 本仓）  
- [30-silicon-valley-doc-standards.md](./30-silicon-valley-doc-standards.md) — 硅谷文档规范  
- [08-multi-project-portfolio.md](./08-multi-project-portfolio.md) — registry 与 harness   策略  
- [17-ceo-cockpit.md](./17-ceo-cockpit.md) — 一屏总览  
- [harness/README.md](../harness/README.md) — 执行 harness 安装  
- [config/company-os-sync-manifest.yaml](../config/company-os-sync-manifest.yaml) — 同步清单  
