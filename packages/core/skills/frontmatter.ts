import { parse as parseYaml } from "yaml";

// Keeping the trailing newline inside the capture group matters: yaml's `|`
// clip chomping only preserves a final newline when the input itself contains
// one. Mirrors the regex used by the Go side in `server/internal/skill`.
const FRONTMATTER_RE = /^---\r?\n([\s\S]*?\r?\n)---\r?\n?/;

export type SkillFrontmatter = Record<string, string>;

export interface ParsedFrontmatter {
  frontmatter: SkillFrontmatter | null;
  body: string;
}

export function parseFrontmatter(raw: string): ParsedFrontmatter {
  const match = FRONTMATTER_RE.exec(raw);
  if (!match) return { frontmatter: null, body: raw };

  const yamlBlock = match[1]!;
  const body = raw.slice(match[0].length);

  let parsed: unknown;
  try {
    parsed = parseYaml(yamlBlock);
  } catch {
    return { frontmatter: null, body };
  }

  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return { frontmatter: null, body };
  }

  const result: SkillFrontmatter = {};
  for (const [key, value] of Object.entries(parsed as Record<string, unknown>)) {
    if (value == null) continue;
    if (typeof value === "string") {
      result[key] = value;
    } else if (typeof value === "number" || typeof value === "boolean") {
      result[key] = String(value);
    } else {
      result[key] = JSON.stringify(value);
    }
  }

  return {
    frontmatter: Object.keys(result).length > 0 ? result : null,
    body,
  };
}
