# 23 — 本机 Agent 运行环境

> **层级**：通用（CEO 机器 / 派单机）  
> **产品仓 CI** 环境单独维护；本文解决 **本地 `cursor-agent` + portfolio dispatch** 的 BLOCKED:INFRA。

---

## 适用范围

- `portfolio-dispatch.sh`
- `autopilot-dispatch.sh`
- `ceo-nightly.sh` 后台派单
- `verify-hands-off.sh`

工程产品派单 **仅** 本机 `cursor-agent` session；不使用 Cursor Cloud API。

## 前置条件清单

| 项 | 验收 |
|----|------|
| `cursor-agent` 已登录 | `cursor-agent status` 或能成功派单 |
| `gh` 已登录 | `gh auth status` |
| 本机 path | `resolve-repo-path.sh --id <id>` exit 0 |
| pnpm / node | 产品 `CLAUDE.md` 命令可跑 |
| 代理（可选） | GitHub 走代理；飞书在 `no_proxy` |

---

## Node / pnpm

- 与产品栈一致（Next 项目常用 Node 22）
- **registry**：派单前在本机验证

```bash
pnpm config get registry
pnpm install   # 在目标产品目录试跑
```

常见 `BLOCKED:INFRA`：`ECONNREFUSED`、错误 registry、lockfile 与 Node 版本不符。

**处理：** 修本机 `~/.npmrc` / `pnpm` 配置 → 重 dispatch；不要把密钥写进 repo。

---

## 代理（multica HQ）

复制并编辑（gitignore）：

```bash
cp .ai-company/config/proxy.env.example .ai-company/config/proxy.env
```

示例（与仓库一致）：

```bash
export https_proxy=http://127.0.0.1:7897
export http_proxy=http://127.0.0.1:7897
export all_proxy=socks5://127.0.0.1:7897
export no_proxy=open.feishu.cn,feishu.cn,larksuite.com,127.0.0.1,localhost
```

- **GitHub / git push**：走代理
- **飞书 webhook / Bot API**：`no_proxy` 必须含飞书域名（`lib/notify.sh` 用 `curl --noproxy`）

`local.env` 可 `source` proxy；见 `lib/source-local-env.sh`。

---

## 并发上限

| 参数 | 默认 | 说明 |
|------|------|------|
| `AUTOPILOT_MAX_CONCURRENT` | 2 | 白天 autopilot |
| 本机 CLI 派单 | 串行 | 多张票顺序跑，非并行 VM |
| `max_total` nightly | 见 registry | 一轮最多派几张 |

**禁止：** 为求快擅自把并发调到 5+（撞车、worktree、BLOCKED 激增）。

安静时段：**23:00–06:00**（Asia/Shanghai）autopilot 不派（`--force` 可覆盖）。

---

## 夜间 cron 与机器

| 项 | 规范值 |
|----|--------|
| nightly 时间 | **21:00** Asia/Shanghai（`install-nightly-cron.sh`） |
| 机器 | 21:00 勿睡眠；或 cron 跑在常开主机 |
| 日志 | `~/.multica/ceo-nightly.log`、`ceo-nightly-dispatch.log` |

`company-defaults.yaml` 里若写其他 cron，**以 runbook + crontab 为准**（21:00）。

---

## 通知

| 渠道 | 配置 |
|------|------|
| 飞书 Bot 私聊 | `setup-feishu-bot-notify.sh` → `feishu-bot-notify.env` |
| 飞书审批（可选） | `setup-feishu-approval.sh` |

CEO 日常 **只看飞书**；无 BLOCKED 不回复。

---

## `local.env` 建议（HQ）

```bash
export CEO_NIGHTLY_DISPATCH=1
export CEO_AUTO_MERGE=1
export CEO_SYNC_BACKLOG=1
# export CEO_FEISHU_APPROVAL_PUSH=1

export AI_REPO_PATH_landing_tool_a=/path/to/landing-tool-a
export AI_REPO_PATH_beatscape=/path/to/MusicSaas
```

---

## INFRA 排查速查

| 现象 | 查 |
|------|-----|
| 派单立刻 BLOCKED | `~/.multica/autopilot-logs/`、产品目录 `pnpm install` |
| 飞书发不出 | proxy / `notify.sh`、Bot token |
| gh 失败 | `gh auth login`、代理 |
| 找不到仓 | `AI_REPO_PATH_*`、`resolve-repo-path.sh --id` |
| nightly 没跑 | crontab、`ceo-nightly.log`、机器睡眠 |

修好后：去 `agent-blocked` → 澄清 comment → 重打 `agent-safe`。

---

## 相关文档

- [HANDS-OFF-COMPLETE.md](../HANDS-OFF-COMPLETE.md)  
- [21-label-state-machine.md](./21-label-state-machine.md)（`BLOCKED:INFRA`）  
- [runbooks/employee-autopilot.md](../runbooks/employee-autopilot.md)  
