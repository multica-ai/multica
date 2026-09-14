# Work Role Format Specification v3.0

A structured, human-readable format for defining AI coding agent work roles as `.md` files with YAML frontmatter. Designed for Claude Code agent personas, Multica agent instructions, and any CLI-based coding agent.

**Sibling spec**: [CHARACTER_FORMAT.md](CHARACTER_FORMAT.md) (v2.0, game characters — includes personality simulation fields).

## Design Philosophy

See [DESIGN_LOGIC.md](DESIGN_LOGIC.md) for the full rationale.

Work roles are **constraint systems**, not personality simulations:
- Every field must produce an observable behavioral difference
- No narrative psychology fields (no MBTI, no trauma, no emotional states)
- The goal is reliable, bounded, predictable agent behavior

## File Structure

```
---
# YAML frontmatter — structured role data (25 fields)
id: engineer
name: 软件工程师
version: "3.0"
...
---

# Role Description
(Free-form Markdown — role context, workflow, output standards)

# 进化日志

> System-appended evolution entries.
> Each entry: date, session ID, project knowledge, permission gaps, workflow patterns.
```

## YAML Frontmatter Fields

### Meta

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | ✅ | Unique identifier (e.g., `architect`, `engineer`) |
| `name` | string | ✅ | Display name (e.g., `系统架构师`) |
| `version` | string | — | Format version (default `"3.0"`) |
| `author` | string | — | Creator name |
| `tags` | string[] | — | Classification tags |
| `role_type` | string | ✅ | `work` (always for work roles) |
| `preferred_model` | string | — | Recommended model: `opus` / `sonnet` / `haiku` |
| `avatar_url` | string | — | Optional avatar image URL |

### Work Style

How the role appears and behaves in work contexts.

| Field | Type | Description |
|-------|------|-------------|
| `work_style` | string | Surface-level work approach. e.g. "全局视角，系统性思维" |
| `work_style_example` | string | Concrete behavioral example. e.g. "被问到'A还是B'时，先分析上下文和约束再回答" |
| `inherent_bias` | string | Systematic tendency or blind spot. e.g. "对过度设计和技术债务一样敏感" |
| `bias_manifestation` | string | When/how the bias surfaces. e.g. "有时会过度分析一个简单问题" |
| `failure_mode` | string | Known failure pattern. e.g. "分析阶段耗时过长" |
| `failure_trigger` | string | What triggers the failure mode. e.g. "面对多方案选择时倾向于深入分析而非快速决策" |

### Goals

What the role aims to accomplish and how.

| Field | Type | Description |
|-------|------|-------------|
| `primary_goal` | string | Main objective. e.g. "做出可解释、可验证的架构决策" |
| `goal_strategy` | string | How they pursue the goal. e.g. "全局扫描 → 识别关键路径 → 多方案对比 → 推荐" |
| `secondary_goal` | string | Secondary objective. e.g. "识别和降低系统风险" |
| `on_success` | string | Expected output when successful |
| `on_failure` | string | Behavior when blocked or failed |

### Communication

How the role communicates.

| Field | Type | Description |
|-------|------|-------------|
| `speaking_style` | string | Overall communication style. e.g. "结构化输出，先结论后分析" |
| `sentence_length` | string | Output verbosity: `short` / `medium` / `long` |
| `forbidden_phrases` | string[] | Phrases the role will never use |
| `golden_lines` | string[] | Example dialogue lines (few-shot reference) |

### Knowledge

What the role knows and doesn't know.

| Field | Type | Description |
|-------|------|-------------|
| `knowledge_areas` | string | Areas of expertise. e.g. "系统设计、安全架构、代码审计" |
| `knowledge_blindspots` | string | What they don't know. e.g. "不了解项目未公开的业务约束" |

### Ethics & Boundaries

Behavioral guardrails.

| Field | Type | Description |
|-------|------|-------------|
| `professional_ethics` | string | Ethical framework. e.g. "架构伦理——可维护性优先于便利性" |
| `red_lines` | string[] | Unbreakable rules. Each must be verifiable |

### Signature Method

The role's unique capability — a specific work pattern that distinguishes it.

| Field | Type | Description |
|-------|------|-------------|
| `signature_method` | string | Unique work approach. e.g. "Tournament决策——3个独立subagent各捍卫一个方案" |
| `signature_trigger` | string | When to activate it. e.g. "面临'选A还是B'且影响超过一个模块时" |

### Tools

Claude Code tool permissions.

| Field | Type | Description |
|-------|------|-------------|
| `allowed_tools` | string[] | Permitted tools with optional Bash subcommand restrictions |
| `denied_tools` | string[] | Blocked tools |

Tool format: `ToolName` or `ToolName(subcommand pattern)`. Examples:
- `Read`, `Grep`, `Glob`, `WebSearch`, `WebFetch`, `Agent`
- `Bash(git *)` — allow git, deny everything else
- `Bash(npm *)`, `Bash(npx *)`
- `Edit(**/*.css)` — allow editing only CSS files

---

## Evolution Log

The `# 进化日志` section is appended by the system at runtime. For work roles, it accumulates:

- **Project knowledge**: things learned about the specific project (e.g. "uses pnpm not npm")
- **Permission gaps**: tools that needed allowlisting during the session
- **Workflow patterns**: effective process discoveries (e.g. "run baseline tests before refactoring")
- **Skill adjustments**: skills whose prompts need tuning for this project

Format:

```markdown
## 2026-06-30 16:01 | Session: auth-refactor

### 项目知识
- 此项目使用 pnpm 管理依赖
- 测试命令: pnpm test -- --coverage

### 权限缺口
- 需添加: `Bash(pnpm *)`

### 工作模式发现
- 重构前先跑一遍现有测试建立基线

### 会话统计
- changed_files: 8
- tests_passed: 47
```

---

## Usage

```python
from character_md import load_character, deploy_persona

# Load a work role
card = load_character("roles/engineer.md")

# Deploy to Claude Code persona directory
result = deploy_persona(
    "roles/engineer.md",
    "/e/claude-personas",
    session_id="auth-refactor",
    learnings={"project_knowledge": ["uses pnpm"]}
)
# → creates /e/claude-personas/engineer/CLAUDE.md + settings.local.json
```

---

## v2.0 → v3.0 Migration

| v2.0 field | v3.0 field | Action |
|-----------|-----------|--------|
| `personality_surface` | `work_style` | Rename |
| `personality_surface_example` | `work_style_example` | Rename |
| `personality_hidden` | `inherent_bias` | Rename |
| `personality_hidden_leak` | `bias_manifestation` | Rename |
| `personality_weakness` | `failure_mode` | Rename |
| `personality_weakness_trigger` | `failure_trigger` | Rename |
| `moral_framework` | `professional_ethics` | Rename |
| `moral_red_lines` | `red_lines` | Rename |
| `special_ability` | `signature_method` | Rename |
| `special_ability_trigger` | `signature_trigger` | Rename |
| `personality_model` | — | Remove |
| `core_lie`, `core_truth`, `formative_trauma`, `core_fear`, `core_desire` | — | Remove |
| `hidden_motivation` | — | Remove |
| `emotional_default`, `emotional_range`, `emotional_triggers` | — | Remove |
| `catchphrase`, `catchphrase_probability` | — | Remove |
| `appearance_hint` | — | Remove |
| `relationship_defaults`, `special_responses` | — | Remove |

The loader is backward-compatible: it parses any YAML frontmatter into a dict. The claude_adapter normalizes both v2.0 and v3.0 field names.

---

## Complete Example

See [roles/engineer.md](roles/engineer.md) for a full work role definition.
