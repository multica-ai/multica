# @multica/cerebro-skill-mention

Cerebro-only TipTap extension that adds a `/skill` trigger to ContentEditor
input fields. Selecting a skill inserts a `[Skill name](mention://skill/<id>)`
reference — the same shape as `@`-mentions for members/agents/issues but
without any side effect.

The extension reuses the existing `mention` ProseMirror node defined by
upstream `BaseMentionExtension`, so the markdown roundtrip is handled by the
shared tokenizer (`mention://(\w+)/<id>` accepts any type).

## Public surface

- `createSkillMentionExtension(queryClient)` — TipTap extension factory.
  Registered alongside `BaseMentionExtension` in `packages/views/editor/extensions/index.ts`.
- `SkillMentionChip` — the inline chip rendered in both the editor (via
  `MentionView`) and readonly content (via `ReadonlyContent`).

Feature-flagged behind `cerebro_skill_mention` (default ON). When the flag
is off the suggestion popup never opens.
