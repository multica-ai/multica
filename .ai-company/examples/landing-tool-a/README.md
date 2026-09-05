# 示例产品线：出海工具落地页（landing-tool-a）

轻量 **静态/营销站** 范例：无游戏逻辑、无 API，适合作为「第二个站」验证 harness 可复制性。

```bash
# 从 multica 仓生成新项目（在 Projects 目录下）
bash multica/scripts/ai-company/scaffold-landing.sh ../landing-tool-a

# 一键 GitHub + labels + backlog issues
bash multica/scripts/ai-company/bootstrap-project.sh ../landing-tool-a \
  --repo YOUR_ORG/landing-tool-a \
  --create-repo --push \
  --sync-backlog --from TICKET-001 --to TICKET-004
```

| 文件 | 说明 |
|------|------|
| [brief.md](./brief.md) | 单页工具站 MVP |
| [backlog.md](./backlog.md) | 4 个 agent-safe ticket |
| [accept_cases.md](./accept_cases.md) | 验收命令（仅前端） |
