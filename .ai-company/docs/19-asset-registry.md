# 19 — 公司资产台账

> **层级**：通用（字段定义 + 维护流程）  
> **与 `project-registry.yaml` 分工**：registry = **调度**（进 git）；资产细节 = **本机 + 可选登记**（路径/域名不进 git）

---

## 三类台账

| 台账 | 文件 | 进 git | 用途 |
|------|------|:------:|------|
| **调度台账** | `templates/project-registry.yaml` | ✅ | priority、cap、paused、tier、repo |
| **本机路径** | `config/repo-paths.local.yaml` | ❌ | 各产品 checkout 绝对路径 |
| **资产明细** | `config/company-assets.local.yaml` | ❌ | 域名、CF 项目、Secret **名**（非值） |

**原则：** 密钥值、token、`.env` **永不**进 multica git。只登记「叫什么、在哪配」。

---

## 调度台账（`project-registry.yaml`）

**必填（脚本已读）：**

| 字段 | 说明 |
|------|------|
| `id` | registry 主键，对应 `AI_REPO_PATH_<id>` |
| `repo` | `github.com/owner/name` |
| `tier` | `production` / `staging` / `experiment` |
| `priority` | 夜间公平队列权重 |
| `max_nightly_tickets` | 每晚上限 |
| `delivery_slug` | `.delivery/<slug>/` |
| `paused` | `true` 则不派单 |

**可选（文档/指挥舱用，脚本可忽略）：**

| 字段 | 说明 | 示例 |
|------|------|------|
| `stack` | 技术栈标签 | `next-static` |
| `openapi` / `e2e` | 门禁开关 | `true` / `false` |
| `notes` | 人读备注 | MLX human-only |
| `domain` | 生产域名 | `json.example.com` |
| `cloudflare_project` | Pages 项目名 | `json-site` |
| `sla_hours` | BLOCKED CEO 响应 | production `4` |
| `kind` | `product`（默认）或 `content` | `content` |
| `dispatch_mode` | 内容线：`remote-pull` / `gha` | `remote-pull` |
| `content_workbench_url` | 内容审稿 HQ（默认 `hq.revoices.app/#content/review`） | 见 `company-defaults.yaml` → `content.workbench_url` |
| `portfolio_group` | UI 分组（如 `openworld`） | `openworld` |
| `workbench_url` | 外部生产站 / Hermes 深链 | `https://www.nowifiwebgames.com` |

示例（注释写在项目块旁，不必所有仓都有）：

```yaml
  - id: json-site
    repo: github.com/chenzh/json-site
    tier: experiment
    domain: json-formatter.example.com    # 可选，指挥舱展示
    cloudflare_project: json-site
    delivery_slug: json-site
```

---

## 本机路径（`repo-paths.local.yaml`）

```bash
cp .ai-company/config/repo-paths.local.yaml.example .ai-company/config/repo-paths.local.yaml
```

```yaml
beatscape: /Users/you/Desktop/MusicSaas
landing-tool-a: /Users/you/Projects/landing-tool-a
chenzh/json-site: /Users/you/Projects/json-site
```

等价：`local.env` 里 `export AI_REPO_PATH_beatscape=...`  
解析：`resolve-repo-path.sh --id <id>`

---

## 资产明细（`company-assets.local.yaml`）

```bash
cp .ai-company/config/company-assets.local.yaml.example .ai-company/config/company-assets.local.yaml
```

结构见 [company-assets.local.yaml.example](../config/company-assets.local.yaml.example)。

**登记内容：**

- 生产 / staging 域名、DNS 托管（Cloudflare 等）
- GitHub Secrets **名称**（如 `SLACK_WEBHOOK_URL`，不写值）
- 本机 only：Stripe dashboard 项目名、飞书群名
- 灾备：上次密钥轮换日期、负责人

**不登记：** API key 明文、`.env` 内容、用户 PII。

---

## tier 与 SLA（对齐 [blocked-triage](../runbooks/blocked-triage.md)）

| tier | BLOCKED CEO 响应 | 夜间派单 | 杀线 |
|------|------------------|----------|------|
| `production` | 4h | 默认 on | OPC 杀线需显式 paused |
| `staging` | 24h | on | 可降 cap |
| `experiment` | 72h | on | 随时 `paused: true` |

`paused: true`：**执行面**停止派单；与 OPC「杀线」对齐时在 `notes` 写原因。

---

## 维护节奏

| 何时 | 动作 |
|------|------|
| 新产品接入 | registry + repo-paths + assets 模板填一行 |
| 上线新域名 | 更新 `company-assets.local.yaml` + 项目 brief |
| 每周 CEO | 扫 registry priority/cap/paused；对照 OPC map |
| 换机 / 灾备 | 先恢复 `repo-paths` + `local.env`，再 `verify-hands-off` |
| 规范/资产文档变更 | 不涉及产品仓 sync（除非改 company-os 文档） |

---

## 指挥舱展示（P1.5+）

`:9477` 可读：

- `project-registry.yaml` → 表：id、repo、tier、paused、priority
- 本机 `company-assets.local.yaml`（若存在）→ 域名 / CF 列
- `resolve-repo-path.sh` → 本机 path 是否就绪

---

## 相关文档

- [08-multi-project-portfolio.md](./08-multi-project-portfolio.md)  
- [22-git-and-remotes.md](./22-git-and-remotes.md)  
- [28-norm-layers.md](./28-norm-layers.md)  
- [16-disaster-recovery.md](./16-disaster-recovery.md)  
