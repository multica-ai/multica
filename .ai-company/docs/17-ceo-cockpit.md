# 17 — CEO 指挥舱（脱手 + 一屏总览）

> **决策（2026-08-29）**：脱手靠脚本与 cron；「一目了然」靠把 `:9477` 升级为 **Company HQ Cockpit**，**不**把 Multica 产品 UI 改成公司操作系统。  
> 与 [HANDS-OFF-COMPLETE.md](../HANDS-OFF-COMPLETE.md) 配套：前者是验收清单，本文是界面与分阶段路线图。

---

## 目标

同时满足两件事：

1. **完全脱手**：21:00 nightly、白天 autopilot、飞书只推异常；CEO 无 BLOCKED 时不回复。
2. **一屏总览**：想「一眼看全公司」时，只开一个本地界面——资产、规范、流程健康、队列脉搏。

二者 **分工实现**，不塞进一个万能产品里硬做。

---

## 推荐架构

```text
                    ┌─────────────────┐
  推（异常）        │  飞书 Bot 私聊   │  ← 日常唯一必看（拉模式可不看任何 UI）
                    └────────▲────────┘
                             │ nightly / BLOCKED / 空转告警
┌──────────────┐    ┌────────┴────────┐    ┌──────────────┐
│ .ai-company/ │───▶│  CEO 指挥舱      │◀───│ GitHub 各仓   │
│ 规范/流程文档 │    │  :9477（拉）     │    │ Issues/PR    │
└──────────────┘    └────────┬────────┘    └──────────────┘
                             │
                    ┌────────▼────────┐
                    │ cron + 脚本层    │  ← 脱手真相源
                    │ nightly/autopilot│
                    └────────┬────────┘
                             │
                    ┌────────▼────────┐
                    │ Multica :3000   │  ← 仅：单 workspace Agent 并发 / daemon
                    └─────────────────┘
```

### 界面分工（固定，勿混用）

| 入口 | 角色 | 何时打开 |
|------|------|----------|
| **飞书 Bot** | 推：日报 + BLOCKED + 可选审批卡 | 每天必看（无告警可 0 回复） |
| **http://127.0.0.1:9477** | 拉：**唯一 CEO 入口**（工程队列 + 资产 + 流程灯 + **内容线** + **OpenWorld 系**） | 想「一眼看全公司」时 |
| **https://hq.revoices.app/** | 拉：**内容 pack 审稿**（lighthouse；从 `:9477` 一键打开） | 审自媒体稿、过 pack 时 |
| **https://hermes.nowifiwebgames.com** / **www.nowifiwebgames.com** | OpenWorld：Hermes 面板 / MetaViewer 生产站（从 `:9477` 深链） | 看出海 App、No WiFi 站 |
| **http://localhost:3000** Multica | 单 workspace 运行时（Agent / Autopilot / daemon） | 调试并发、看单次 run |
| **GitHub** | 交付真相源（Issues / PR / labels / CI） | 深入某票、审 merge |
| **SecondBrain OPC map** | 经营真相源（主线 / 杀线 / 值得做） | 周度战略，见 [13-opc-bridge.md](./13-opc-bridge.md) |

**原则**：飞书负责 **推**；指挥舱负责 **拉**；长文规范住在 `.ai-company/`，界面 **索引 + 状态 + 少量按钮**，不复制全文。

---

## 指挥舱一屏应有什么

在现有 `ceo-workbench.sh`（Portfolio / Site Factory / 派单）之上，增加 **「公司概览」** 分区（或独立页签）。各区块只 **聚合**，真相源如下：

| 区块 | 展示内容 | 真相源 |
|------|----------|--------|
| **脉搏** | BLOCKED / RUNNING / QUEUE / 昨夜 merge 摘要 | `ceo-dashboard.sh` |
| **资产** | 产品线 id、repo、本机 path、tier、paused、priority/cap | `templates/project-registry.yaml` + `resolve-repo-path.sh` |
| **规范** | 质量门禁、Visual Gate、merge 规则、分级 | 链到 [07-quality-gates.md](./07-quality-gates.md)、[06-task-grading.md](./06-task-grading.md)、`CLAUDE.md`、`.delivery/README` |
| **流程** | 21:00 cron 是否在、上次 nightly 成功、autopilot 上次运行、`verify-hands-off` 灯 | `verify-hands-off.sh`、`~/.multica/ceo-nightly.log` |
| **运行** | Multica daemon 并发、本机 cursor-agent 进程 | `multica-runtime-status.sh` |
| **动作** | 智能派单、试跑 nightly（`--no-dispatch`）、打开某 repo | 现有 workbench API / 脚本 |
| **OPC** | 本周主线 / 杀线（只读外链） | SecondBrain `*-map-portfolio-opc.md` |
| **OpenWorld** | `openworld` monorepo + `metadata-viewer` 路径、队列、Hermes / 生产站链接 | `portfolio_group: openworld` in registry |

### 规范入口（指挥舱固定链，实现时照抄）

| 标题 | 路径 |
|------|------|
| 愿景与躺平定义 | [docs/01-vision.md](./01-vision.md) |
| 运营模型（Sleep Mode） | [docs/02-operating-model.md](./02-operating-model.md) |
| 质量门禁 | [docs/07-quality-gates.md](./07-quality-gates.md) |
| 完成定义 DoD | [docs/18-definition-of-done.md](./18-definition-of-done.md) |
| 好票写法 | [docs/20-issue-brief-style-guide.md](./20-issue-brief-style-guide.md) |
| Label / BLOCKED | [docs/21-label-state-machine.md](./21-label-state-machine.md) |
| Git / fork | [docs/22-git-and-remotes.md](./22-git-and-remotes.md) |
| 本机 Agent 环境 | [docs/23-local-agent-environment.md](./23-local-agent-environment.md) |
| 资产台账 | [docs/19-asset-registry.md](./19-asset-registry.md) |
| 规范同步 | [docs/27-norm-sync.md](./27-norm-sync.md) |
| CEO 每日 | [runbooks/ceo-daily.md](../runbooks/ceo-daily.md) |
| 员工 Autopilot | [runbooks/employee-autopilot.md](../runbooks/employee-autopilot.md) |
| BLOCKED 分拣 | [runbooks/blocked-triage.md](../runbooks/blocked-triage.md) |
| 脱手验收 | [HANDS-OFF-COMPLETE.md](../HANDS-OFF-COMPLETE.md) |
| 接入完成清单 | [ONBOARDING-DONE.md](../ONBOARDING-DONE.md) |
| 规范分层 | [docs/28-norm-layers.md](./28-norm-layers.md) |

---

## 分阶段实施

### 阶段 0 — 脱手共识（必须先绿）

与 [HANDS-OFF-COMPLETE.md](../HANDS-OFF-COMPLETE.md) 一致：

| # | 项 | 验收 |
|---|-----|------|
| 1 | `bash scripts/ai-company/verify-hands-off.sh` | 全绿 |
| 2 | pnpm / npm registry 可用 | 派单不因 install  BLOCKED |
| 3 | 21:00 机器常开 / 防睡眠 | `~/.multica/ceo-nightly.log` 有昨夜记录 |
| 4 | 日常只看飞书 | 无 BLOCKED → 不回复 |
| 5 | 推送只用 `push-fork.sh` | 禁止 `git push origin`（403） |

→ **此时已能睡**；指挥舱可以仍是「够用版」，不阻塞脱手。

### 阶段 1 — 指挥舱 MVP（1～2 周，优先产品化）

**目标**：`:9477` 成为唯一 **拉** 界面；飞书仍是唯一 **推** 界面。

| 交付物 | 验收 |
|--------|------|
| 「公司概览」页签 | registry 表格 + repo 外链 + paused 标记 |
| 流程灯 | verify 结果、cron 安装态、上次 nightly 时间戳 |
| 规范入口 | 上表 8 个固定链接 |
| 脉搏嵌入 | 复用 dashboard JSON，不必另开终端 |

**不做**：在 Multica issue 里复刻 portfolio；不在 UI 里托管长文 wiki。

实现落点：

- API：`scripts/ai-company/ceo-workbench-server.py`
- 前端：`scripts/ai-company/workbench/`

### 阶段 2 — 脱手加深（与 UI 并行）

| 能力 | 文档 |
|------|------|
| backlog 同步、reconcile、work-finder | [runbooks/work-finder.md](../runbooks/work-finder.md) |
| 飞书审批卡（Human 档） | [runbooks/feishu-approval.md](../runbooks/feishu-approval.md) |
| 白天 autopilot | [runbooks/employee-autopilot.md](../runbooks/employee-autopilot.md) |

指挥舱仅多几个绿灯与日志 tail，不改变 CEO 日常「只看飞书」。

### 阶段 3 — 暂不做（除非规模值得）

| 项 | 原因 |
|----|------|
| Multica issue ↔ GitHub label **双向同步** | 产品级工程，非 OPC 共识 |
| 用 Multica UI **替代** 指挥舱 | 无 `project-registry` 多仓视图；见 [14-multica-autopilot-portfolio.md](./14-multica-autopilot-portfolio.md) P2 |
| 自研「硅谷级」CEO 大面板 | 编排 + 告警 + GitHub 真相源已够用；参考开源 OPC 实践（OpenHands / SWE-agent 作 Worker，非 Dashboard） |

---

## 做完阶段 0 + 1 后你会得到什么

| 能力 | 说明 |
|------|------|
| ✅ 完全脱手 | 在 BLOCKED 可飞书解的前提下，靠 cron，不靠盯盘 |
| ✅ 一屏总览 | 资产、队列、流程健康、规范入口 — 只开 `:9477` |
| ✅ Multica 不打架 | `:3000` 只管 runtime，不做公司 OS |
| ❌ 非 Figma 级完美 | 够用、可维护优先 |
| ❌ 非文档替代品 | 规范仍在 `.ai-company/`，界面只链过去 |

**工作台 UI（P1.5）：** 打开 `:9477` 顶部 **公司指挥舱** — 流程灯（cron / verify / nightly）、规范文档链接、资产表（本机 path、company-os 副本版本、域名/CF、队列）。API：`GET /api/company-overview`；按钮 **重跑脱手验收** 调 `verify-hands-off.sh`。

---

## 日常口诀

```text
飞书 0 告警 → 不打开浏览器
想一眼看公司 → 只开 http://127.0.0.1:9477
要改某张票 → GitHub Issue/PR
要改战略主线 → SecondBrain OPC map
```

启动指挥舱：

```bash
bash scripts/ai-company/ceo-workbench.sh
# → http://127.0.0.1:9477
```

---

## 相关文档

- [12-ceo-dashboard.md](./12-ceo-dashboard.md) — 脉搏信号与终端 dashboard  
- [13-opc-bridge.md](./13-opc-bridge.md) — OPC 经营面 ↔ 执行面  
- [13-implementation-roadmap.md](./13-implementation-roadmap.md) — P0→P3 总路线图（含指挥舱阶段 1）  
- [HANDS-OFF-COMPLETE.md](../HANDS-OFF-COMPLETE.md) — 脱手验收清单  
- [27-norm-sync.md](./27-norm-sync.md) — 规范同步三层管道  
- [28-norm-layers.md](./28-norm-layers.md) — 通用 vs 项目 vs 任务  
