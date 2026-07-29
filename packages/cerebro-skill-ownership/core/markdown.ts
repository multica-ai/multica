export interface SkillMarkdownParts {
  frontmatter: string;
  body: string;
}

const FRONTMATTER_PREFIX_RE = /^(---\r?\n[\s\S]*?\r?\n---\r?\n?)/;

/**
 * Separates the YAML prefix from a skill document before it enters the
 * rich-text editor. Tiptap owns Markdown body formatting, while this prefix
 * must round-trip byte-for-byte for the skill loader.
 */
export function splitSkillMarkdown(content: string): SkillMarkdownParts {
  const match = FRONTMATTER_PREFIX_RE.exec(content);
  if (!match) return { frontmatter: "", body: content };

  const frontmatter = match[1]!;
  return { frontmatter, body: content.slice(frontmatter.length) };
}
