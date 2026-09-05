# Compliance Checklist — music-game-sea

**原则：脚本门禁优先；Hermes 只做评审报告，不替代下列 exit code。**

## CI 必跑（agent-safe ticket 可改相关代码）

| ID | 检查 | 实现建议 | 命令 |
|----|------|----------|------|
| C1 | Cookie 同意横幅 | DOM/Playwright 或 RTL | `pnpm test compliance/cookie-banner.test.ts` |
| C2 | Privacy + Terms 链接可达 | 请求 `/privacy` `/terms` 200 | `pnpm test compliance/privacy-links.test.ts` |
| C3 | 无第三方跟踪在未同意时加载 | mock analytics，断言未 inject | `pnpm test compliance/tracking-consent.test.ts` |
| C4 | API 不返回多余 PII | handler 测试断言字段集 | `make test` 内 `TestScoreResponse_NoEmail` |

## 部署前人工（CEO，human-only）

| ID | 项 | 负责人 |
|----|-----|--------|
| H1 | Privacy Policy 文案律师/模板审核 | CEO |
| H2 | demo 曲版权证明归档 | CEO |
| H3 | 生产域名 + DPA（若用分析服务） | CEO |

## Agent 辅助（Hermes Worker，可选）

- 评审 Privacy Policy 是否列出：session id、nickname、score、IP 日志保留期  
- 评审依赖 SDK 列表（Sentry、GA 等）是否在同意前加载  

**Hermes 输出 = PR 评论；不 merge 条件。**

## 地区与内容

- 首版无 geo-block；无未成年人专门模式（landing 标注 18+）  
- nickname profanity：服务端 denylist（可 agent-safe 小列表）  

## 违规处理

合规 CI 失败 → 等同 verify 失败 → BLOCKED，不得 merge。
