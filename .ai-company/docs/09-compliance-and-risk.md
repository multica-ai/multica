# 09 — 合规、安全与风险

## 风险分层

| 等级 | 示例 | Agent 策略 |
|------|------|------------|
| **P0** | 密钥泄露、RCE、支付漏洞 | 立即停止队列；human-only |
| **P1** | PII 处理、GDPR、未成年人 | agent-assisted + 脚本门禁 |
| **P2** | 一般内容站、工具站 | agent-safe（满足分级清单） |

---

## 出海默认检查项（音乐游戏 / 内容站）

**脚本门禁（CI 必跑）：**

- [ ] Cookie / consent 横幅存在性（E2E 或 DOM 测试）
- [ ] 隐私政策、ToS 链接可达
- [ ] `Secure` / `HttpOnly` / `SameSite` cookie 标志（集成测试）
- [ ] CORS 白名单非 `*`（生产配置）
- [ ] 地区限制头 / geo 路由（若产品有）

**Agent 辅助（Hermes，不替代脚本）：**

- 隐私政策文案是否覆盖收集字段
- 第三方 SDK 清单（广告、分析）

---

## 密钥与权限

| 规则 | 说明 |
|------|------|
| Agent 无生产密钥 | runtime 仅 dev/staging |
| API / 第三方密钥在 GHA Secrets | 不写入 repo |
| Multica per-agent MCP | 优先 workspace 全局 MCP；per-agent 配置已知有坑 |
| `.env` 永不 commit | deny merge-policy |

---

## Hermes / OpenClaw 公司政策

| 工具 | 允许 | 禁止 |
|------|------|------|
| Hermes | PR 安全评论、隐私评审报告 | 顶层调度；无人值守自进化技能 |
| OpenClaw | 公开页抓取、合规 checklist 初稿 | 自动 merge；执行任意 shell |

---

## 审计留痕

每个 Agent PR 必须含：

- 链回 Issue / `.delivery/<slug>/`
- `accept_cases` 勾选状态
- Verifier 命令输出
- 使用的模型 / Agent run id（Cursor link）

Multica task 历史 + GitHub Actions logs = 审计链。

---

## 事件响应

见 [runbooks/incident-response.md](../runbooks/incident-response.md)。

## 灾备

文档与脚本进 GitHub；密钥不进明文仓。见 [16-disaster-recovery.md](./16-disaster-recovery.md) 与 [runbooks/disaster-recovery.md](../runbooks/disaster-recovery.md)。

---

## 相关文档

- [07-quality-gates.md](./07-quality-gates.md)  
- [06-task-grading.md](./06-task-grading.md)  
- [16-disaster-recovery.md](./16-disaster-recovery.md) 
