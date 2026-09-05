# Multica + 飞书 双向 CEO 审批

**目标：** BLOCKED / 绿 PR 在 **Multica Web** 和 **飞书** 任一侧批准，状态同步到 GitHub + Multica。

---

## 架构

```text
GitHub agent-blocked / 绿 PR
        ↓ sync
Multica issue（收件箱提醒）
        ↓ push card
飞书卡片 [批准] [打回] [Multica]
        ↓ callback / 文字命令
approval-bridge → gh comment/label + multica comment/status
```

映射表：`~/.multica/ceo-approvals/registry.json`

---

## 一次性配置

```bash
bash scripts/ai-company/setup-feishu-approval.sh --test
```

飞书开放平台 → Bot 应用 → **事件订阅**：

| 项 | 值 |
|---|---|
| Request URL | `https://<公网>/feishu/event` |
| 事件 | `card.action.trigger`, `im.message.receive_v1` |

本机回调（需公网可达 — 推荐 Cloudflare Tunnel）:

```bash
bash scripts/ai-company/ceo-feishu-approval-service.sh install
bash scripts/ai-company/ceo-feishu-cloudflare-tunnel.sh quick-install   # 或 setup-named 稳定域名
bash scripts/ai-company/ceo-feishu-cloudflare-tunnel.sh quick-url       # 飞书 Request URL
```

`local.env` 或独立文件（推荐，避免与 local.env 混写）:

```bash
cp .ai-company/config/feishu-approval.env.example .ai-company/config/feishu-approval.env
# 填入 Verification Token 后:
bash scripts/ai-company/ceo-feishu-approval-service.sh install
bash scripts/ai-company/print-feishu-inbound-setup.sh
```

`local.env` 也可:

```bash
export MULTICA_FRONTEND_URL=http://localhost:3000
export FEISHU_VERIFICATION_TOKEN=...
export CEO_FEISHU_APPROVAL_PUSH=1   # 21:00 日报后自动推卡片
```

---

## 日常用法

### 飞书

- 卡片点 **批准** / **打回**
- 或发文字：
  - `/批 beatscape 42 用方案 A`
  - `/打回 beatscape 42 需要补 brief`

### Multica Web

1. 收件箱打开 `[CEO审批·BLOCKED …]` issue
2. 评论写清决定
3. 终端同步 GitHub：

```bash
bash scripts/ai-company/ceo-feishu-approval.sh approve beatscape 42 与 Multica 评论一致
```

（后续可把 Multica 评论 webhook 也接上，实现 Web 侧全自动。）

### 手动推卡片

```bash
bash scripts/ai-company/ceo-feishu-approval.sh list
bash scripts/ai-company/ceo-feishu-approval.sh sync
bash scripts/ai-company/ceo-feishu-approval.sh push
```

---

## 行为说明

| 类型 | 飞书「批准」 | GitHub | Multica |
|---|---|---|---|
| `agent-blocked` | 去 blocked → agent-safe + comment | ✅ | issue → done + comment |
| 绿 PR | `gh pr merge` | ✅ | issue → done + comment |
| 打回 | comment only | ✅ | issue 保持 blocked + comment |

---

## 故障排查

| 现象 | 处理 |
|---|---|
| 卡片按钮无反应 | 回调服务是否运行；飞书事件订阅是否通过校验 |
| Multica issue 未创建 | `multica login`；本地 API 是否可达 |
| `/批` 无回复 | 同上 + 检查 `im.message.receive_v1` 订阅 |
| 批准了但 Agent 未跑 | 确认 GitHub label 已是 `agent-safe`；再 dispatch |

---

## 相关

- [blocked-triage.md](./blocked-triage.md)
- [nightly-ceo-brief.md](./nightly-ceo-brief.md)
