# Character MD — Work Role Skills

AI agent work role personas as Multica-compatible SKILL.md files.

## Skills

| Skill | ID | Model | Read-only | Signature Method |
|-------|----|-------|-----------|-----------------|
| System Architect | `architect-persona` | Opus | ✅ | Tournament Decision |
| Software Engineer | `engineer-persona` | Sonnet | ➖ | Parallel Sub-agent Orchestration |
| UI/UX Designer | `designer-persona` | Sonnet | ➖ (frontend only) | Multi-Dimensional Parallel Review |
| Tech Researcher | `researcher-persona` | Opus | ✅ | Adversarial Verification |

## Format

All role cards follow the [WORK_ROLE_FORMAT.md v3.0](https://github.com/multica-ai/multica/blob/main/skills/character-md/WORK_ROLE_FORMAT.md) specification — a structured YAML frontmatter + Markdown format with 25 fields covering work style, goals, communication, knowledge boundaries, ethics, and tool permissions.

## Usage

### In Multica (Web UI)

1. Go to Settings → Skills → Import
2. Paste the raw URL of any SKILL.md from this directory
3. Attach the imported skill to any Agent

### With Claude Code (CLI)

```bash
# Copy to Claude Code skills directory
cp -r skills/character-md/architect ~/.claude/skills/architect-persona
```

### Generate from source

```bash
# Regenerate SKILL.md files from role card sources
cd character_md
python -c "
from multica_bridge import deploy_all_roles
deploy_all_roles('roles', '../multica-source/skills/character-md', also_claude_md=False, also_settings=False)
"
```

## Design

These work roles are built on the insight that **AI agent work roles are constraint systems, not personality simulations**. See [DESIGN_LOGIC.md](https://github.com/multica-ai/multica/blob/main/skills/character-md/DESIGN_LOGIC.md) for the full design philosophy.
