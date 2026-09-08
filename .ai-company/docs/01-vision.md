# 01 — 愿景与「躺平」定义

## 我们要建什么

一家 **AI 软件公司**：你担任 CEO，下面是由编排系统、任务平台、编码 Worker、质量门禁组成的「虚拟编制」。  
业务覆盖 **多个网站、多个产品线**（出海音乐游戏站只是其中一条），交付物是可上线、可审计、可复现的代码与制品。

这不是「一个超强 ChatGPT 会话」，而是 **工厂 + 工单 + 硬门禁** 的工业流程。

---

## 「躺平」的正确定义

| 躺平 **不是** | 躺平 **是** |
|---------------|-------------|
| 零介入、零责任 | 只碰 **决策层**，不碰执行层 |
| Agent 自主决定要不要测 | **脚本退出码** 决定能不能合并 |
| 每个站一套 Prompt | **全公司一套 harness**，换项目只换 brief |
| 出问题再逐行读代码 | 只看 **绿/红/BLOCKED** 与仪表盘 |

### CEO 保留的不可委托职能

1. **战略**：做什么产品、服务谁、商业模式、定价。
2. **预算与风险**：月度 API/CI 上限、哪些路径禁止 auto-merge。
3. **验收权威**：`accept_cases.md` 里勾选项的最终确认（可委托「抽检」但不能没有 AC）。
4. **BLOCKED 拍板**：需求歧义、架构分叉、合规定性。
5. **法律责任**：主体、支付、用户数据、出海合规的最终责任人。

### 可完全委托给 AI 公司的

- 需求拆解、技术方案草稿、编码、单测、修 lint、开 PR、写 changelog。
- 队列调度、并发 Worker、失败重试、活动日志。
- 契约校验（OpenAPI）、E2E 回归、路径白名单 auto-merge。
- 竞品抓取初稿、安全扫描报告（**脚本结果为准**）。

---

## 成功标准（公司级 KPI）

| 指标 | 目标（P1 稳定后） |
|------|-------------------|
| `agent-safe` 任务无人值守完成率 | ≥ 70%（先到 PR+CI 绿） |
| 夜间运行 CEO 被叫醒次数 | 仅 BLOCKED / 安全事件 |
| 单 ticket 人工时间 | CEO ≤ 5 分钟（不含 BLOCKED） |
| CI 首次通过率 | 追踪并逐月提升 |
| 单项目交付周期 | brief → merge 中位数可度量 |

---

## 设计原则

1. **真相源在文件**：`brief.md`、`accept_cases.md`、`api_spec.yaml`、OpenSpec 变更包——不是聊天记录。
2. **硬门禁在代码外圈**：编排用 Python/YAML/Shell；LLM 无权跳过 `pytest` / `ruff` / `oasdiff`。
3. **Worker 无流程决策权**：Cursor、Hermes、OpenClaw 只返回工件。
4. **可复制**：新项目 = 复制 harness + 填模板，不换操作系统。
5. **可审计**：每个 PR 绑定 Issue、验证命令输出、merge-policy 决策可追溯。

---

## 不做的反模式

- ❌ 用 Hermes / OpenClaw 做整条流水线总调度  
- ❌ 用 Multica 端到端替代编程式 DAG（任务级 ≠ 用例级）  
- ❌ 无 `accept_cases` 的「你看着办」需求进夜间队列  
- ❌ 关闭 CI required checks 求快  
- ❌ 每上新站重写一套 Agent Prompt 而不更新 harness  

---

## 下一步

- [02-operating-model.md](./02-operating-model.md) — 公司如何每天转  
- [runbooks/ceo-daily.md](../runbooks/ceo-daily.md) — 老板每日动作  
