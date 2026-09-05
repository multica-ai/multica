# 14 — Multica Autopilot 与 Portfolio 夜间调度

公司有两层夜间调度，可 **并存**：

| 层 | 机制 | 管什么 |
|----|------|--------|
| **Portfolio（公司总部）** | CEO 本机 `ceo-nightly.sh` / `portfolio-dispatch.sh` | 按 `project-registry.yaml` 对各 **产品 repo** 本机派单 |
| **产品 repo** | `dispatch-cursor-agent-cli.sh` + gate workflow | 拉本仓 `agent-safe` Issue → 本机 cursor-agent |
| **Multica Autopilot（可选）** | `multica autopilot` cron/webhook | 在 Multica 看板创建 Issue/任务、留 run 历史 |

```text
CEO 维护 project-registry.yaml
        ↓
ceo-nightly (本机 21:00) → portfolio-dispatch --local
        ↓
dispatch-cursor-agent-cli.sh @ org/music-game-sea
dispatch-cursor-agent-cli.sh @ org/landing-tool-a
        ↓
各产品仓 PR → agent-delivery-gate（GHA）
```

---

## A. Portfolio 调度（推荐先上）

### 1. 填台账

编辑 [templates/project-registry.yaml](../templates/project-registry.yaml)：

- `repo:` 改为真实 `github.com/YOUR_ORG/...`（脚本会自动剥前缀）
- `priority` / `max_nightly_tickets` / `paused`

### 2. 本地试跑

```bash
bash scripts/ai-company/portfolio-dispatch.sh --dry-run
bash scripts/ai-company/portfolio-dispatch.sh --max-total 3
```

### 3. 启用 CEO 本机夜间 cron

```bash
bash scripts/ai-company/install-nightly-cron.sh --install
# 21:00 → ceo-nightly.sh → portfolio-dispatch（本机 cursor-agent）
```

### 3b. 白天自主迭代（macOS，推荐）

```bash
bash scripts/ai-company/autopilot-launchagent-service.sh install
# GUI 会话内每 30min → autopilot-dispatch（reconcile + merge + 派单）
# 完整方案：[33-autonomous-iteration.md](./33-autonomous-iteration.md)
```

`portfolio-agent-dispatch.yml`（GHA）**不用于派单**；若误触发会提示改在本机运行。

可选：`PORTFOLIO_GH_TOKEN` 仅用于跨 repo 的 `gh` 操作（merge/reconcile），与 Cursor 无关。

### 4. 各产品仓必备

每个产品 repo 仍需：

- CEO 本机 `cursor-agent login` + `AI_REPO_PATH_*`（或 `resolve-repo-path.sh`）
- labels + `agent-delivery-gate.yml`（harness 已带）

---

## B. Multica Autopilot（看板 + 历史）

适合：你想在 **Multica UI** 看 run、用 webhook 接 GitHub/Linear。

### 创建 Autopilot（CLI）

```bash
# 自托管 Multica 已 login 后
multica autopilot create \
  --title "Portfolio — music-game-sea nightly" \
  --description "Read .delivery/music-game-sea/brief.md and CLAUDE.md. Process issues labeled agent-safe. Verifier must exit 0." \
  --agent <YOUR_RUNTIME_AGENT_UUID> \
  --mode create_issue

multica autopilot trigger-add <AUTOPILOT_ID> \
  --kind schedule \
  --cron "0 2 * * *" \
  --timezone Asia/Shanghai
```

为 **每个产品线** 各建一个 Autopilot，或一个总 Autopilot 其 prompt 指向 `project-registry.yaml`（Agent 读文件决定去哪仓——软逻辑，仅辅助）。

### Webhook 触发（GitHub → Multica）

1. Multica UI → Autopilot → Add **Webhook** trigger  
2. GitHub repo webhook → POST JSON（如 `issue.labeled` 含 `agent-safe`）  
3. 配置 **Event filters** 避免每个 push 都跑  

与 Portfolio 本机派单 **分工**：

- **本机 portfolio-dispatch**：硬触发、公平配额、不依赖 LLM  
- **Multica Autopilot**：可视化、人工 `@agent`、run 审计  

### 生成命令脚本

```bash
bash scripts/ai-company/print-multica-autopilot-commands.sh
```

输出各项目的 `multica autopilot create` 命令（需填 agent id）。

---

## C. 推荐组合（躺平 CEO）

| 时段 | 发生什么 |
|------|----------|
| 21:00 | `ceo-nightly` → `portfolio-dispatch` 按 registry 分配 slot（本机） |
| 夜间 | 各仓 cursor-agent 跑子流水线 + CI |
| 早上 | CEO `ceo-daily.md`：BLOCKED + merge 勾选 |

**不必** 先上 LangGraph；队列 <10 ticket/天，本机 portfolio + gate 足够。

---

## 故障排查

| 现象 | 处理 |
|------|------|
| portfolio workflow 误触发 | GHA 上不会派单；在本机跑 `portfolio-dispatch.sh` |
| 某 repo 从不 dispatch | `paused: true`？repo 路径错？无 agent-safe issue？ |
| 重复 dispatch | 产品仓 `agent-running` label 是否正常打上 |
| Multica 与本机双跑 | 关 Autopilot schedule 或只保留 ceo-nightly |

---

## 相关

- [08-multi-project-portfolio.md](./08-multi-project-portfolio.md)  
- [config/company-defaults.yaml](../config/company-defaults.yaml)  
- [runbooks/ceo-daily.md](../runbooks/ceo-daily.md)  
