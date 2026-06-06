export type Frontmatter = { name: string; description: string };

// Mirrors server parseSkillFrontmatter (server/internal/handler/skill.go):
// only the leading `--- ... ---` block, line-prefixed name:/description:.
export function parseFrontmatter(content: string): Frontmatter {
  const result: Frontmatter = { name: "", description: "" };
  if (!content.startsWith("---")) return result;
  const end = content.indexOf("---", 3);
  if (end < 0) return result;
  const block = content.slice(3, end);
  for (const raw of block.split("\n")) {
    const line = raw.trim();
    if (line.startsWith("name:")) {
      result.name = stripQuotes(line.slice("name:".length).trim());
    } else if (line.startsWith("description:")) {
      result.description = stripQuotes(line.slice("description:".length).trim());
    }
  }
  return result;
}

function stripQuotes(s: string): string {
  return s.replace(/^["']|["']$/g, "");
}
