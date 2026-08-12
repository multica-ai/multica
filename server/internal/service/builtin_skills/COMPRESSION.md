# Builtin skills compression — evidence & provenance

This change compresses the 9 builtin `SKILL.md` files and their 8
`references/*-source-map.md` support files. Content semantics are preserved;
only prose density, repetition, and low-signal phrasing were reduced.
Every file was processed and validated with `skill_compressor.py` (contract:
inventory of literals → candidate → validate with word-ratio ≥ 2.0 →
independent semantic review), and the repo's own conformance tests
(`TestBuiltinSkillsConformToTemplate`, `TestBuiltinSkillsFrontmatterIsStrictYAML`,
`Test*SkillCovers*`) pass on this tree.

## Token impact (tiktoken, o200k_base)

| File | Before | After | x |
|---|---|---|---|
| multica-autopilots/SKILL.md | 906 | 536 | 1.69 |
| multica-autopilots/references/autopilots-source-map.md | 864 | 560 | 1.54 |
| multica-creating-agents/SKILL.md | 4268 | 2460 | 1.73 |
| multica-creating-agents/references/creating-agents-source-map.md | 4476 | 1704 | 2.63 |
| multica-mentioning/SKILL.md | 3044 | 1639 | 1.86 |
| multica-mentioning/references/mentioning-source-map.md | 4280 | 1825 | 2.35 |
| multica-onboarding/SKILL.md | 1584 | 856 | 1.85 |
| multica-projects-and-resources/SKILL.md | 1225 | 733 | 1.67 |
| multica-projects-and-resources/references/projects-and-resources-source-map.md | 860 | 340 | 2.53 |
| multica-runtimes-and-repos/SKILL.md | 1487 | 909 | 1.64 |
| multica-runtimes-and-repos/references/runtimes-and-repos-source-map.md | 1274 | 683 | 1.87 |
| multica-skill-importing/SKILL.md | 2672 | 1617 | 1.65 |
| multica-skill-importing/references/skill-importing-source-map.md | 2901 | 1331 | 2.18 |
| multica-squads/SKILL.md | 2400 | 1421 | 1.69 |
| multica-squads/references/squad-source-map.md | 3231 | 2010 | 1.61 |
| multica-working-on-issues/SKILL.md | 3439 | 2058 | 1.67 |
| multica-working-on-issues/references/working-on-issues-source-map.md | 3330 | 1848 | 1.80 |
| **Total** | **42241** | **22530** | **1.87 (−46.7%)** |

Builtin skills are embedded into the server binary and written into every
task workdir; agents read them on demand via their runtime's skill loader.
The SKILL.md bodies and source-maps never enter the context window
automatically (only names + descriptions are indexed), but slimmer files
mean cheaper on-demand reads and less disk per task.

## Reproducing

```bash
# per-file validation (word-ratio ≥ 2.0, literals present verbatim)
python3 skill_compressor.py validate <original> <candidate> \
  --inventory inventory.json --root <dir>

# conformance (this tree)
cd server && go test ./internal/service -run \
  'TestBuiltinSkillsConformToTemplate|TestBuiltinSkillsFrontmatterIsStrictYAML|Test.*SkillCovers|Test.*Skill\(' -count=1
```

Base: `13706d6` (main). Tooling: skill_compressor.py from the workspace
`workflow-base` skill bundle; per-file artifacts (inventory.json,
semantic-review.json) retained locally at
`.build-tmp/builtins-compress/roots/<skill>/`.
