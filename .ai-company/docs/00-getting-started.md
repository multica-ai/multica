# 00 — 快速开始（CEO 第一周）

按顺序执行，**7 天内**从 0 到「第一个项目可夜间跑 agent-safe」。

---

## Day 1 — 读宪法（2h）

- [ ] [01-vision.md](./01-vision.md)
- [ ] [02-operating-model.md](./02-operating-model.md)
- [ ] [03-organization.md](./03-organization.md)
- [ ] [04-architecture.md](./04-architecture.md)
- [ ] [28-norm-layers.md](./28-norm-layers.md) — 通用 / 项目 / 任务放哪
- [ ] [29-harness-layout.md](./29-harness-layout.md) — examples vs templates vs harness
- [ ] [30-silicon-valley-doc-standards.md](./30-silicon-valley-doc-standards.md) — 硅谷文档纪律（SSOT / 英文代码面）

## Day 2 — 搭基础设施（4h）

- [ ] Cursor Business + API Key
- [ ] GitHub org：Secrets、`agent-*` labels、branch protection
- [ ] 自托管 [Multica](https://github.com/multica-ai/multica)（或暂用纯 GHA 路径 B）
- [ ] Slack webhook（可选）
- [ ] 填 [config/company-defaults.yaml](../config/company-defaults.yaml)

## Day 3 — 接入第一个项目（4h）

- [ ] 跟 [onboard-new-project.md](../runbooks/onboard-new-project.md)
- [ ] 复制 harness + 模板
- [ ] trivial agent-safe ticket 手动 dispatch 跑通

## Day 4 — 子 Agent 与门禁（2h）

- [ ] 确认 `.cursor/agents/*` 就位
- [ ] [07-quality-gates.md](./07-quality-gates.md) 对齐 CI job 名
- [ ] 试一次故意 BLOCKED（模糊 brief）

## Day 5 — 第二个项目（复制验证）（3h）

- [ ] 用同一 harness 接入第二站（证明可复制）
- [ ] 更新 [project-registry.yaml](../templates/project-registry.yaml)

## Day 6 — 夜间队列（1h）

- [ ] 启用 cron 或 Multica Autopilot
- [ ] `nightly.enabled: true`（确认预算护栏）

## Day 7 — 躺平演习（15min）

- [ ] 只跑 [ceo-daily.md](../runbooks/ceo-daily.md)，不打开 IDE 写代码
- [ ] 记录：BLOCKED 数、merge 数、是否被无谓告警吵醒

---

## 第一个月里程碑

| 周 | 目标 |
|----|------|
| W1 | 上表完成 |
| W2 | ≥2 项目进 registry；夜间稳定 |
| W3 | agent-safe 完成率 >50% |
| W4 | 首份 CEO 周报；评估 LangGraph |

详见 [13-implementation-roadmap.md](./13-implementation-roadmap.md)。
