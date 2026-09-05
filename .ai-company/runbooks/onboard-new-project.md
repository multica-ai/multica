# 新项目 / 新网站接入 Runbook

预计 **1 个工作日** 完成 harness 接入，**3 天** 内首个 agent-safe ticket 跑通。

---

## Phase 0 — 决策（CEO，30 min）

- [ ] 项目 id、repo URL、tier（production / experiment）
- [ ] 技术栈（Next / Go / 纯静态等）
- [ ] 是否需要 OpenAPI、E2E
- [ ] 登记 [project-registry.yaml](../templates/project-registry.yaml)
- [ ] 本机：`repo-paths.local.yaml` + [company-assets.local.yaml](../config/company-assets.local.yaml.example)（见 [19-asset-registry.md](../docs/19-asset-registry.md)）

---

## Phase 1 — Harness 复制（1～2 h）

**推荐：一键安装**

```bash
bash /path/to/multica/.ai-company/harness/install.sh /path/to/new-repo
# 或从 multica 根目录：
bash scripts/ai-company/install-harness.sh /path/to/new-repo
```

**手动复制（等价）：**

```bash
cd /path/to/new-repo

# 从 multica 或 company-harness 复制
cp -r /path/to/multica/.delivery .
cp -r /path/to/multica/.cursor/agents .cursor/
mkdir -p .github/workflows
cp /path/to/multica/.github/workflows/agent-delivery-*.yml .github/workflows/
cp -r /path/to/multica/scripts/agent-delivery scripts/

# 公司模板 / 示例
mkdir -p .delivery/<project-slug>
cp /path/to/multica/.ai-company/examples/music-game-sea/brief.md .delivery/<project-slug>/   # 或改用自己的
cp /path/to/multica/.ai-company/templates/accept_cases.md .delivery/<project-slug>/
```

- [ ] 编写项目 `CLAUDE.md`（复制 [templates/CLAUDE.project.md](../templates/CLAUDE.project.md)，填 stack / 命令 / forbidden paths）
- [ ] 确认 `.cursor/rules/company-harness.mdc` 已安装（`install-harness.sh` 默认复制；勿把 company-os 全文写进规则）
- [ ] 若有 `.secondbrain`：`sync-all-harness.sh` → `vault-harness.mdc` + `zbrain-session.mdc`
- [ ] `bash …/sync-company-norms.sh --id <project-id>` → `.delivery/company-os/`
- [ ] 调整 `merge-policy.json` deny/allow
- [ ] 若有 API：放 `api/openapi.yaml`，加 contract workflow

---

## Phase 2 — 标签与通知（30 min）

**CEO 本机：** `cursor-agent login`

**GitHub（可选 Secrets）：**

| Secret | 必需 |
|--------|------|
| `SLACK_WEBHOOK_URL` | 推荐 |

**Labels：** `agent-safe`, `agent-running`, `agent-blocked`, `agent-done`

**Branch protection：** required checks 与 CI job 名对齐

---

## Phase 3 — Multica（可选，30 min）

```bash
# 自托管 Multica 已 setup 前提下
multica autopilot create \
  --title "<project-slug> nightly" \
  --description "Process agent-safe issues. Read .delivery/ and CLAUDE.md." \
  --agent <runtime-agent-id> \
  --mode create_issue
```

记录 `autopilot_id` 到 project-registry。

---

## Phase 4 — 试跑（2～4 h）

1. 创建 **trivial agent-safe** Issue（如：补 README 测试）
2. CEO 本机：`bash scripts/agent-delivery/dispatch-cursor-agent-cli.sh <N>`
3. 验证：PR 开 → CI 绿 → gate 行为符合 policy
4. CEO 勾 AC → merge

---

## Phase 5 — 纳入夜间队列

- [ ] `install-nightly-cron.sh --install`（CEO 本机 21:00）
- [ ] 或启用 Multica Autopilot schedule
- [ ] 飞书/Slack 测试消息收到

---

## 验收标准

- [ ] trivial ticket 无人值守到 CI 绿
- [ ] BLOCKED 路径试过（故意模糊 brief → `NEED_CLARIFY`）
- [ ] project-registry 已提交
- [ ] CEO daily runbook 可跨该项目执行

---

## 故障排查

| 现象 | 查 |
|------|-----|
| dispatch 失败 | `cursor-agent login`、本机 `AI_REPO_PATH_*`、`resolve-repo-path.sh` |
| CI 不触发 | workflow path、default branch |
| auto-merge 未执行 | `merge-policy.json`、branch 前缀 `cursor/` |
| Agent 跳过测试 | Verifier prompt、required checks |
