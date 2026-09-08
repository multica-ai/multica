# 28 — 规范分层（通用 vs 项目 vs 任务）

> 与 [27-norm-sync.md](./27-norm-sync.md) 配套：27 讲 **怎么同步**；28 讲 **写什么、放哪、谁维护**。  
> 项目 `CLAUDE.md` 骨架：[templates/CLAUDE.project.md](../templates/CLAUDE.project.md)

---

## 一句话

| 层级 | 回答的问题 | 权威位置 |
|------|------------|----------|
| **通用** | 工厂怎么运转？ | multica `.ai-company/` → sync → `.delivery/company-os/` |
| **项目** | 这个产品是什么、怎么验？ | 产品仓 `.delivery/<slug>/` + `CLAUDE.md` |
| **任务** | 这一票做到哪算完？ | GitHub Issue（+ 链到 brief/AC） |

**冲突时优先级：** 任务 > 项目 brief > 项目 CLAUDE > 通用 company-os。

---

## 四层地图

```text
层 0  SecondBrain / OPC          层 1  Company OS（通用）        层 2  项目              层 3  任务
      值不值得做                       工厂规则                      产品线宪法            单票范围

权威  Vault HARNESS/                multica .ai-company/          product/.delivery/      GitHub Issue
      03-MAPS OPC map

副本  VAULT-HARNESS.md              .delivery/company-os/         <slug>/* 本仓          Issue body

维护  你 / OPC 周回顾               CEO + harness 变更            CEO 投 brief          triage + 派单前

同步  sync-all-harness.sh           sync-company-norms.sh         不 sync（本仓真相）     不同步
```

---

## 层 1 — 通用规范（Company OS）

**放什么：** 换任意产品线都仍然成立的规则。

| 类型 | 示例 | 路径 |
|------|------|------|
| 愿景与运营 | Sleep Mode、三层时钟 | `docs/01`、`02` |
| 任务分级 | agent-safe 清单 | `docs/06` |
| 质量门禁 | Verifier、CI、Visual Gate | `docs/07` |
| 组织与权限 | 谁有调度权 | `docs/03` |
| BLOCKED / Autopilot | 分拣、白天派单 | `runbooks/*` |
| 空模板 | brief、AC 骨架 | `templates/*` |

**不放什么：**

- 某站的竞品列表、Stripe 密钥名、Next 还是 CF Pages
- `project-registry.yaml`、飞书 env、CEO 脚本（HQ 本机）

**下发：** [company-os-sync-manifest.yaml](../config/company-os-sync-manifest.yaml) + `sync-company-norms.sh`。

**新增通用文档流程：**

1. 在 `.ai-company/docs/` 编号落盘（P0 示例：`18` DoD、`20` 好票、`21` 状态机、`22` Git、`23` 本机环境）
2. 追加 [company-os-sync-manifest.yaml](../config/company-os-sync-manifest.yaml)
3. `sync-company-norms.sh`
4. 各产品仓 commit `.delivery/company-os/`

---

## 层 2 — 项目规范（单产品线）

**放什么：** 只有这个 repo / 这个 `delivery_slug` 成立的内容。

```text
<product-repo>/
  CLAUDE.md                         # 栈、命令、禁止路径（从 CLAUDE.project.md 复制填写）
  .delivery/
    <slug>/
      brief.md                      # In/Out of scope、用户与地区
      accept_cases.md               # 本产品级 DoD / 验收命令
      backlog.md                    # 待投队列（→ Issues）
      competitor_inventory.md       # 复刻站
      wont_do.md                    # 复刻站
      api_spec.openapi.yaml         # 有 API 时
      human-only-queue.md           # 支付等 human-only 票
      compliance-checklist.md       # 出海合规（按产品选用）
    config/
      merge-policy.json             # 在通用 deny 上微调
    company-os/                     # 层 1 只读副本（勿手改）
```

### multica `examples/<slug>/` 的角色

**种子包**，不是运行时真相。接入后复制到产品仓 `.delivery/<slug>/`，并填写根目录 `CLAUDE.md`（见 [templates/CLAUDE.project.md](../templates/CLAUDE.project.md)）。

```bash
cp -r multica/.ai-company/examples/music-game-sea product/.delivery/music-game-sea
# 之后只改产品仓；要更新种子再回头改 examples/（可选）
```

### 项目 `CLAUDE.md` 最低要求

从 [templates/CLAUDE.project.md](../templates/CLAUDE.project.md) 复制到产品仓根目录，必填：

- Stack、Commands、Forbidden paths
- `delivery_slug`、链到 `.delivery/<slug>/brief.md`
- 链到 `.delivery/company-os/README.md`（通用规范副本）

**禁止：** 把 `06-task-grading.md` 全文复制进 `CLAUDE.md` — 只链 `company-os/`。

---

## 层 3 — 任务规范（单张 Issue）

**放什么：** 生命周期 = 一张票。

| 字段 | 要求 |
|------|------|
| Title | 建议 `[Agent]: <动词> <对象>` |
| What & why | ≤3 句 |
| AC | **可执行命令** + 勾选；或明确「见 accept_cases.md §X」 |
| Out of scope | 必填（Issue 模板已要求） |
| Labels | `agent-safe` 等；夜间只拉 safe 且无 blocked/running |

**规则：** 未分级不得进夜间队列（见 `06-task-grading`）。模糊 brief → Planner `NEED_CLARIFY`。

---

## 通用 vs 项目：判定表

| 问题 | 通用 | 项目 | 任务 |
|------|:----:|:----:|:----:|
| agent-safe 判定标准 | ✅ | 补充禁止项 | 本票是否满足 |
| Verifier 必须 exit 0 | ✅ | 具体命令写 CLAUDE/AC | 本票跑哪些命令 |
| merge-policy 默认 deny | ✅ | `merge-policy.json` 微调 | — |
| Visual Replica 流程 | ✅ | inventory + wont_do | 本票组件 ID |
| Stripe / 支付 | ✅ human-only 定义 | human-only-queue | PAY-xxx 单票 |
| 技术栈 Next/Go/CF | — | ✅ brief + CLAUDE | — |
| OPC 本周杀线 | 桥接说明 `13-opc-bridge` | — | — |

**口诀：** 换产品线仍成立 → 通用；只此 repo → 项目；只此 Issue → 任务。

---

## Agent 阅读顺序

产品仓派单 / Cloud Agent kickoff 按序读（越后越具体，冲突以后者为准）：

1. 仓库根 `CLAUDE.md`
2. `.delivery/<slug>/brief.md`、`accept_cases.md`（复刻 + `competitor_inventory.md`、`wont_do.md`）
3. `.delivery/company-os/docs/06-task-grading.md`
4. `.delivery/company-os/docs/07-quality-gates.md`
5. `.delivery/company-os/docs/18-definition-of-done.md`
6. 当前 **GitHub Issue** 正文（AC、out of scope）
7. `.cursor/agents/*.md`
8. `.delivery/prompts/orchestrator-kickoff.md`（流水线阶段）

Kickoff 模板（产品仓）：[templates/orchestrator-kickoff-product.md](../templates/orchestrator-kickoff-product.md)  
`install-harness.sh` 安装到非 multica 仓时会写入 `.delivery/prompts/orchestrator-kickoff.md`。

---

## CEO 维护节奏

| 周期 | 动作 |
|------|------|
| 改通用规则 | 编辑 `.ai-company/` → manifest → `sync-company-norms.sh` |
| 改 Vault 编码习惯 | SecondBrain → `sync-all-harness.sh` |
| 改 Agent prompt / workflow | `install-harness.sh --force` 各产品 |
| 新项目上線 | onboard runbook + `examples/` 种子 + `CLAUDE.project.md` |
| 每周 | 更新 `project-registry` priority/cap；OPC 杀线对照 `13-opc-bridge`；升格 [harness-candidates](./system-evolution/harness-candidates.md)（见 [31-harness-learnings-routing](./31-harness-learnings-routing.md)） |
| 每票投前 | triage `06-task-grading` 清单 |

---

## 反模式（禁止）

- ❌ 产品仓自建 `.ai-company/docs/` 全文副本（用 `company-os/`）
- ❌ `examples/` 与产品仓 `brief.md` 双写同一段 AC
- ❌ 通用规范写进 Issue 而不更新层 1 文档
- ❌ 口头「差不多」覆盖 `accept_cases` 命令
- ❌ 把 multica `CLAUDE.md` 整份复制到静态落地页仓

---

## 相关文档

- [19-asset-registry.md](./19-asset-registry.md) — 调度台账 vs 本机资产  
- [27-norm-sync.md](./27-norm-sync.md) — 同步命令与 manifest  
- [29-harness-layout.md](./29-harness-layout.md) — **按类型放哪、examples 与 harness**  
- [30-silicon-valley-doc-standards.md](./30-silicon-valley-doc-standards.md) — 硅谷文档纪律（与 conventions 对齐）  
- [31-harness-learnings-routing.md](./31-harness-learnings-routing.md) — **经验回流路由与候选队列**  
- [32-opc-harness-knowledge-design.md](./32-opc-harness-knowledge-design.md) — **设计方案总览（HQ）**  
- [08-multi-project-portfolio.md](./08-multi-project-portfolio.md) — registry 与 harness  
- [runbooks/onboard-new-project.md](../runbooks/onboard-new-project.md) — 接入时创建层 2  
- [templates/CLAUDE.project.md](../templates/CLAUDE.project.md) — 项目 CLAUDE 模板  
