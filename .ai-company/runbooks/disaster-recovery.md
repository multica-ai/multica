# 灾备与恢复（Disaster Recovery）

> **原则**：代码与公司脑子 → GitHub；钥匙 → 加密离线/保险库；本机是工厂不是金库。

## GitHub 放什么 / 不放什么

| 放进 GitHub（本仓 `fork` = `chenzh/multica`） | 绝不进明文 Git |
|-----------------------------------------------|----------------|
| `.ai-company/docs/**`（含 system-evolution） | `*.env`、token、API key |
| `.ai-company/runbooks/**`、templates、harness | `local.env` / `proxy.env` / 飞书 env |
| `scripts/ai-company/**` | `~/.multica/config.json` 真值、cloudflared 凭证 |
| `*.env.example`、`company-defaults.yaml` | 客户生产密钥、支付密钥 |
| 本文件（恢复顺序） | 未加密的备份包 |

产品代码各自仓：`zstock` / `json-site` / `landing-tool-a` / … → 各自 GitHub remote。  
**未建 remote 的产品仓 = 灾备空洞**，优先补仓再干活。

## RPO / RTO（当前阶段）

| 资产 | RPO | RTO | 载体 |
|------|-----|-----|------|
| 公司文档 + 脚本 | ≤1 天（每次改完 push） | 分钟级 clone | GitHub `chenzh/multica` |
| 产品代码 | ≤1 天（理想跟 PR push） | 1–2h | 各产品 GitHub |
| 密钥 | ≤1 天（加密包） | 半天内填回 `.env` | 1Password / `.age` 离线 + 云盘 |
| 公网入口 | URL 可变 | 0.5–2h 重建 tunnel | Cloudflare（优先 named tunnel） |
| Autopilot 运行态 | 尽力 | 可不恢复 | `~/.multica` 可重建 |

## 本机关键面（Mac Host Inventory）

恢复时按此核对，**不含密钥**：

### LaunchAgents（公司相关）

- `com.multica.selfhost`
- `com.multica.ceo-workbench`
- `com.multica.ceo-feishu-approval`
- `com.multica.ceo-feishu-cloudflare`
- `com.cloudcli` / `com.cloudcli.tunnel` / `com.cloudcli.tunnel-sync`
- `com.zstock.daemon` / `com.zstock.local` / `com.zstock.quick-tunnel`

### 路径约定

| 角色 | 路径 |
|------|------|
| 公司 OS 仓 | `~/Projects/multica` |
| 产品仓根 | `~/Projects/*`（部分在 `~/Desktop`，见 `repo-paths.local.yaml`） |
| Multica 运行态 | `~/.multica/` |
| Autopilot 日志 | `~/.multica/autopilot-logs/` |
| 飞书桥接工作区 | `~/Documents/feishu-cursor-workspace`（个人记忆；公司文档不沉这里） |
| CloudCLI | `~/Projects/claudecodeui` |

### 公网入口（名称会变，以本机最新为准）

- CloudCLI：`https://agent.revoices.app/`（Worker + tunnel）
- zstock：本机 `:8600` + Cloudflare 公网映射
- CEO 飞书 approval / workbench：见 `local.env.example` 与对应 LaunchAgent

## 新机恢复顺序（Checklist）

1. **装工具**：git、bun/node、Docker（若自托管需要）、cloudflared、Cursor CLI  
2. **clone 公司仓**：`git clone https://github.com/chenzh/multica.git ~/Projects/multica`  
3. **拉产品仓**：按 `project-registry` / `local.env.example` 列表 clone 到 `~/Projects`  
4. **填密钥**（从保险库，不从 Git）：  
   `cp .ai-company/config/*.env.example` → 对应 `.env`，粘贴真值  
5. **路径映射**：`repo-paths.local.yaml`（本机路径）  
6. **起 Multica selfhost + daemon**（见 `scripts/local-selfhost-autostart.sh` / LaunchAgent）  
7. **装 LaunchAgents**：cloudcli / zstock / feishu approval（对照上文清单）  
8. **验活**：`multica` 能列 issue；Autopilot dry-run；一个公网站点 200  
9. **飞书桥接**（可选）：恢复 `feishu-cursor-workspace` 记忆与 `cron-jobs.json`

## 日常纪律（让 GitHub 真能当灾备）

1. 改 `.ai-company` 文档/脚本后 **当天 push 到 `fork`（chenzh/multica）**  
2. 产品仓禁止「只在本机 commit 不 push」超过 24h  
3. 密钥轮换后 **同步更新加密保险库**，不要只改本机文件  
4. 每季度按本 runbook 在干净环境演练一次（至少走到步骤 8）

## 与事件响应的边界

- 泄露 / 恶意依赖 / 生产事故 → [incident-response.md](./incident-response.md)  
- 机器丢了 / 盘挂了 / 换机 → **本文件**

## 密钥灾备（GitHub 外）

推荐任选其一（可并存）：

1. **1Password / 系统钥匙串**：条目与 `*.env.example` 字段对齐  
2. **age 加密包**：本机生成 → 存云盘/U 盘；密文可另存私有仓库（仍禁止明文）  

目录预留：`.ai-company/backups/`（已 gitignore）。脚本后续可加 `backup-secrets.sh`。
