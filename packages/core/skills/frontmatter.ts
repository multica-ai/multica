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

/** Only return names whose scalar interpretation agrees with the Go importer. */
export function skillImportReconciliationName(
  raw: string,
  fallback: string,
): string | null {
  const match = FRONTMATTER_RE.exec(raw);
  if (!match) return raw.startsWith("---") ? null : fallback;
  try {
    // YAML 1.1 also resolves timestamps and legacy numeric forms. Treat those,
    // structured values and malformed documents as unknown rather than guess
    // how Go will coerce them to a name.
    const parsed: unknown = parseYaml(match[1]!, { version: "1.1", mapAsMap: true });
    const seen = new Set<unknown>();
    const hasSafeKeys = (value: unknown): boolean => {
      if (!value || typeof value !== "object") return true;
      if (seen.has(value)) return false;
      seen.add(value);
      if (value instanceof Map) {
        return [...value].every(([key, entry]) => typeof key === "string" && hasSafeKeys(entry));
      }
      return !Array.isArray(value) || value.every(hasSafeKeys);
    };
    // Go rejects collection keys; JS would otherwise stringify them and keep
    // a sibling name even when the server discards the entire frontmatter.
    if (!(parsed instanceof Map) || !hasSafeKeys(parsed)) return null;
    const name: unknown = parsed.get("name");
    if (name == null) return fallback;
    if (typeof name !== "string") return null;
    // Go strings.TrimSpace includes NEL but excludes BOM, unlike JS trim().
    const space = "[\\t-\\r \\u0085\\u00a0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000]";
    return name.replace(new RegExp(`^${space}+|${space}+$`, "g"), "") || fallback;
  } catch {
    return null;
  }
}
