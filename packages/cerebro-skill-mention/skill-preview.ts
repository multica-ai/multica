/**
 * Turns a raw SKILL.md `content` body into a compact, readable preview for the
 * side panel of the `/` skill picker.
 *
 * Skill bodies open with a YAML frontmatter block (`---\nname: …\n---`) whose
 * fields (name, description) are already shown elsewhere in the picker, so we
 * strip it and show the instructions that follow — that is the part that
 * actually answers "what can this skill do". Bodies routinely run tens of KB,
 * so the result is capped to keep the popup light and the DOM small.
 */
const PREVIEW_CHAR_LIMIT = 1500;

export function skillPreviewText(content: string | null | undefined): string {
  if (!content) return "";
  // Drop a single leading YAML frontmatter block if present.
  const withoutFrontmatter = content.replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n?/, "");
  const body = withoutFrontmatter.trim() || content.trim();
  if (body.length <= PREVIEW_CHAR_LIMIT) return body;
  return `${body.slice(0, PREVIEW_CHAR_LIMIT).trimEnd()}…`;
}
