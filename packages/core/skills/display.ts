/**
 * skillDisplayName returns the human-readable label for a skill.
 *
 * `name` is the ASCII-slug identity (filesystem dir name, runtime match key);
 * `display_name` is an optional UTF-8 label (e.g. Chinese) used only for UI
 * rendering. When `display_name` is absent or blank we fall back to `name` so
 * the identity is always shown. Accepts the narrow `AgentSkillSummary` shape
 * too, since both share `name` + optional `display_name`.
 */
export function skillDisplayName(skill: {
  name: string;
  display_name?: string;
}): string {
  const display = skill.display_name?.trim();
  return display ? display : skill.name;
}
