# OPC ↔ AI 公司 OS 桥接

> 把 SecondBrain「一人公司经营」接到 Multica `.ai-company/` 执行面。  
> 更新：2026-08-26

## 两套系统怎么分工

| 层 | 真相源 | 你每天碰什么 |
|----|--------|--------------|
| **经营（OPC）** | `Documents/SecondBrain/03-MAPS/...-map-portfolio-opc.md` | 本周全力主线、杀线、触达/转发 |
| **交付（Company OS）** | `Projects/multica/.ai-company/` + 各产品仓 Issues | 队列、派单、BLOCKED、PR/CI |
| **控制台** | `ceo-workbench.sh` → http://127.0.0.1:9477 | 指挥舱：队列 / 资产 / 流程灯 / 点派单（见 [17-ceo-cockpit.md](./17-ceo-cockpit.md)） |

**原则**：OPC 决定「做什么产品值得做」；Company OS 决定「怎么无人值守做完」。不要用 Agent 会话当经营真相源。

## 当前 OPC 杀线（2026-08-20）

- TickFocus：有反馈才恢复全力  
- 全力主线：**No WiFi SEO 抄数**（非加功能）  
- Company OS 侧：MusicSaas/BeatScape 可并行跑 **agent-safe** 工程票，但不抢 OPC「增长抄数」心智带宽

## 每日 15 分钟（合并版）

**自动（推荐）**：21:00 cron 跑 `ceo-nightly.sh`，飞书/Slack 收日报；你只回 BLOCKED。

1. 开工作台：`bash ~/Projects/multica/scripts/ai-company/ceo-workbench.sh`  
2. 或手动：`bash ~/Projects/multica/scripts/ai-company/ceo-dashboard.sh`  
3. 处理 BLOCKED → 绿 PR 勾 AC → merge  
4. OPC：若本周主线仍是 No WiFi → 只做「抄数/记账」，不新开 TickFocus 功能票  
5. 日记一行写入 `Documents/SecondBrain/05-DAILY/YYYY-MM-DD.md` + 飞书工作区 `.cursor/memory/`

## 口令

| 你说 | 系统做 |
|------|--------|
| `CEO 仪表盘` | 跑 ceo-dashboard / 开工作台 |
| `派活` | portfolio-dispatch --local 或工作台「智能派单」 |
| `本周 OPC 回顾` | 读 OPC map + WBR + 杀线状态 |
| `杀线清除` | 仅在有 TickFocus 转发记账后 |

## 产品线登记（执行面）

见 `.ai-company/templates/project-registry.yaml`：

- **beatscape** → `chenzh/MusicSaas`（主力工程队列）  
- landing-tool-a / saas-stripe-mvp → 有本机 clone 才可本地派单  

经营面产品（TickFocus / No WiFi / BlackBox）仍以 SecondBrain 项目笔记为准；接入 Company OS 时再 `install-harness.sh`。
