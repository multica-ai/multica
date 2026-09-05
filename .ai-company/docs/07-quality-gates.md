# 07 — 质量门禁规范

**原则：门禁由脚本与 CI 执行；Agent 只负责跑命令并粘贴输出，无权宣布通过。**

---

## 门禁层级

```text
L1 本地/Agent 环：Verifier 子 Agent 跑验收命令
L2 PR CI：required checks（与 L1 命令应对齐）
L3 契约：OpenAPI lint + breaking diff
L4 E2E：Playwright 关键路径（游戏站/支付流等）
L5 发布：merge-policy + 可选人工 AC 勾选
```

**任一层 exit ≠ 0 → 不得 merge。**

---

## L1 — Verifier 最低栏

每个 `accept_cases.md` 必须列出命令，例如：

```bash
pnpm test --filter @scope/package
make test
make check   # 全量验证前
```

Verifier 规则（见 `.cursor/agents/verifier.md`）：

1. 逐条执行 AC 中的命令。
2. PR body **必须粘贴**最后一次成功运行的完整输出。
3. exit ≠ 0 → 输出 `BLOCKED`，交还 Implementer，**最多 3 轮**。
4. 3 轮后仍失败 → `agent-blocked`，告警 CEO。

---

## L2 — CI Required Checks

每个项目仓库 `main` branch protection：

| Check | 说明 |
|-------|------|
| `frontend` | pnpm typecheck + test + lint |
| `backend` | go test / make test |
| `contract` | OpenAPI（若项目有 API） |
| `e2e` | Playwright（按项目启用） |

Agent PR 与人工 PR **同一标准**。

---

## L3 — API 契约门禁

**适用：** 有公开或前后端分离 API 的项目。

```yaml
# .github/workflows/api-gate.yml（每项目复制）
jobs:
  lint-openapi:
    steps:
      - run: vacuum lint api/openapi.yaml --fail-severity error
  breaking:
    steps:
      - uses: oasdiff/oasdiff-action/breaking@v0
        with:
          fail-on: WARN
```

契约真相源：`api_spec.openapi.yaml`（与实现同步责任在 Implementer + CI）。

---

## L4 — E2E 门禁

**游戏站 / 关键转化路径** 最低集：

- 首页加载、核心玩法一局、登录（若有）、支付沙箱（若有）
- 失败截图上传 CI artifact

E2E **不在 Agent 环内全跑**（太慢）→ CI 跑；Agent 环可跑 smoke 子集。

### L4-视觉 — 复刻 / 落地页完整度（Visual Replica Gate）

**适用：** Site Factory 产出站、竞品复刻、营销落地页。

| 产物 | 必须存在 |
|------|----------|
| `competitor_inventory.md` | 路由 + 组件表 + 交互矩阵 |
| `wont_do.md` | 显式不做 |
| `accept_cases.md` | Structure / Interaction / Visual 三类 AC |
| `make visual-check` | Playwright `@visual` 截图对比 |

规则：

1. Planner 无 `wont_do` / 空 inventory → **NEED_CLARIFY**，不得派 Implementer。
2. Verifier **必须跑** `make visual-check`（或 `pnpm exec playwright test --grep @visual`）；exit ≠ 0 → **BLOCKED**。
3. 禁止口头「很像 / 完整复刻」替代命令输出。
4. 默认容差 `maxDiffPixelRatio ≤ 0.02`；基线提交进 git（`e2e/**/*-snapshots/`）。
5. 不绕过竞品 bot 墙；抓不到则 inventory 标注 fallback 参考站。
6. **不**把 Chromatic/Percy 当硬依赖（先本地 Playwright）。

最低视觉用例：首页 desktop + 移动端 **375** 宽。

---

## L5 — Merge Policy

文件：`merge-policy.json`（模板见 `templates/merge-policy.json`）

```text
PR head 分支前缀 cursor/*
    → 逐文件匹配 deny 列表 → 任一命中 → 禁止 auto-merge
    → 全部落在 allow 列表 → CI 绿 → 可 auto-merge
```

**默认 deny（公司级）：**

- `**/migrations/**`
- `**/auth/**`、`**/payment/**`
- `**/api/**`（若未单独开白名单）
- `.env*`、密钥文件

---

## 合规双层校验（出海）

| 层 | 执行者 | 示例 |
|----|--------|------|
| 脚本（优先） | CI | CORS 配置测试、地区头、cookie 标志 |
| Agent（辅助） | Hermes | GDPR 文案评审、隐私字段清单 |

**脚本失败 = 硬 BLOCK；Agent 意见 = 建议，不替代脚本。**

---

## 禁止事项

- ❌ Verifier 不跑命令写 PASSED  
- ❌ 跳过 required checks merge  
- ❌ `--no-verify`  
- ❌ Agent 修改 `merge-policy.json` 扩大 auto-merge 范围（human-only）
- ❌ 口头宣称「很像 / 干完了」替代 DoD 命令输出
- ❌ 安静时段外无脑刷满本机 cursor-agent 并发（Autopilot 默认 ≤2）

---

## Employee Autopilot（自己干活）

白天由 `scripts/ai-company/autopilot-dispatch.sh` 当值班经理：

- `QUEUE>0` 且未满并发 → `portfolio-dispatch --local`
- 仅 BLOCKED → 不派，升级通知
- 连续派单 QUEUE 不降 → 空转告警
- 23:00–06:00 不自动派（见 [employee-autopilot.md](../runbooks/employee-autopilot.md)）

CEO 职责收缩为：处理 Human 档升级 + 抽检 merge。

## Work-Finder（自己找活）

`scripts/ai-company/work-finder.sh`：QUEUE 低于目标时往已有产品 `backlog.md` 追加 agent-safe 小票并 sync。见 [work-finder.md](../runbooks/work-finder.md)。

---

## 相关文档

- [18-definition-of-done.md](./18-definition-of-done.md)  
- [20-issue-brief-style-guide.md](./20-issue-brief-style-guide.md)  
- [21-label-state-machine.md](./21-label-state-machine.md)  
- [09-compliance-and-risk.md](./09-compliance-and-risk.md)  
- [templates/accept_cases.md](../templates/accept_cases.md)
- [runbooks/employee-autopilot.md](../runbooks/employee-autopilot.md)
- [runbooks/work-finder.md](../runbooks/work-finder.md)
- [runbooks/visual-replica-gate.md](../runbooks/visual-replica-gate.md)
