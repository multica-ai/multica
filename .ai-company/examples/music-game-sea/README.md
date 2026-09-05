# 示例产品线：出海音乐节奏小游戏（music-game-sea）

> 可复制到任意新仓库：  
> `cp -r .ai-company/examples/music-game-sea /path/to/repo/.delivery/music-game-sea`

这是 **CEO 已填好的 brief 范例**，展示「多项目 AI 公司」里一条产品线长什么样。  
技术栈假设：**Next.js 14（App Router）+ Go API + PostgreSQL + Playwright E2E**。

---

## 文件说明

| 文件 | 用途 |
|------|------|
| [brief.md](./brief.md) | 产品意图、范围、禁止路径 |
| [accept_cases.md](./accept_cases.md) | 可勾选验收 + Verifier 命令 |
| [api_spec.openapi.yaml](./api_spec.openapi.yaml) | API 契约真相源 |
| [compliance-checklist.md](./compliance-checklist.md) | 出海合规（脚本优先） |
| [backlog.md](./backlog.md) | 建议拆分的 agent-safe ticket 队列 |

---

## 接入步骤

```bash
# 1. 创建仓库（或已存在）
mkdir -p ../music-game-sea && cd ../music-game-sea && git init

# 2. 安装 harness + 骨架（已预置在 ../music-game-sea 可参考）
bash /path/to/multica/.ai-company/harness/install.sh .

# 3. 复制本示例（若尚未复制）
cp -r /path/to/multica/.ai-company/examples/music-game-sea .delivery/music-game-sea

# 4. 登记公司台账 .ai-company/templates/project-registry.yaml

# 5. 批量建 Issue
bash /path/to/multica/scripts/ai-company/sync-backlog-to-issues.sh \
  --backlog .delivery/music-game-sea/backlog.md \
  --repo YOUR_ORG/music-game-sea \
  --dry-run

# 6. 本地验证 TICKET-001 基线
pnpm install && make check
```

---

## 分级提醒

| 阶段 | 典型 ticket | 分级 |
|------|-------------|------|
| 落地页 + i18n | backlog #1–#3 | agent-safe |
| 核心一局玩法 | #4–#6 | agent-assisted |
| 支付 / 未成年人 | 任意 | human-only |
