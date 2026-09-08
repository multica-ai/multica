# Runbook — 产品情报站上线（好用版）

权威方案：[docs/35-product-intel-lounge.md](../docs/35-product-intel-lounge.md)

## 一句话

**3 Bot + 3 Autopilot + 口令开票；先工程闭环绿，再开情报。**

---

## 上线顺序（照做）

### 0. 工程闭环（必须先绿）

```bash
bash scripts/ai-company/verify-hands-off.sh
bash scripts/ai-company/autopilot-launchagent-service.sh install
bash scripts/ai-company/install-nightly-cron.sh --install
```

### 1. 仓库

```bash
mkdir -p docs/intel
# README + issue template 已在 multica 主仓
```

### 1b. Multica + 标签 + Autopilot（本机）

```bash
bash scripts/ai-company/setup-product-intel-lounge.sh
# 预览：--dry-run
```

### 2. 飞书

- 建群 `产品情报站`
- 置顶群规：`cat .ai-company/templates/intel-lounge/feishu-group-pin.txt`
- 拉 CEO + 可选 1 真人 + 3 Bot（intel-scout / product-analyst / intel-moderator）
- ID 快照：`~/.multica/intel-lounge.json`（`setup-product-intel-lounge.sh` 写入）

### 3. Multica

| 步骤 | 动作 |
|------|------|
| Agent | 创建 `intel-scout`、`product-analyst`、`intel-moderator`、`content-picker` |
| 频道 | 3 个飞书 Bot 分别绑定 scout / analyst / moderator |
| Autopilot A | `0 9 * * 1-5` · 创建 issue · Runbook 见 35 号文 A |
| Autopilot B | `0 14 * * 1-5` · 仅运行 · Runbook 见 35 号文 B |
| Autopilot C | `0 10 * * 6` · 仅运行 · Runbook 见 35 号文 C |
| 主持 | 配置口令：`做 N` `内容 N` `忽略` `下周 N` |

### 4. 试跑 3 天

| 天 | 期望 |
|----|------|
| D1 | 09:00 有 `intel/YYYY-MM-DD-daily` Issue + 情报卡 |
| D2 | 14:00 产品卡；试回 `忽略` 有回执 |
| D3 | 试回 `做 1` → agent-safe 票 + 主持回执 |

### 5. 内容线（可选，晚于 D3）

仅当已回过 `内容 N` 且内容仓就绪：见 [24-content-operations.md](../docs/24-content-operations.md)。

---

## 日常（CEO）

| 动作 | 频率 |
|------|------|
| 扫 09:00 / 14:00 卡片 | 每天 ≤1 分钟 |
| 口令 `做 N` / `内容 N` / `忽略` | 有需要才回，目标 ≤1 次/天 |
| 读 21:00 工程日报 | 已有习惯 |
| 翻历史 | Issue 或 `:9477` 情报页（P1.5） |

---

## 故障

| 现象 | 处理 |
|------|------|
| 卡片格式乱 | 改 Runbook，强调 35 号文四块模板 |
| 一周从没回过口令 | 卡片太长或不准 → 缩「必看」为 2 条 |
| 口令无回执 | 查主持 Bot 绑定与群会话 |
| 热点淹没工程 | 确认情报 `priority: 10` |
| Token 偏高 | 按 35 号文降级顺序关 C → B |

---

## 相关

- [35-product-intel-lounge.md](../docs/35-product-intel-lounge.md)
- [employee-autopilot.md](./employee-autopilot.md)
- [33-autonomous-iteration.md](../docs/33-autonomous-iteration.md)
