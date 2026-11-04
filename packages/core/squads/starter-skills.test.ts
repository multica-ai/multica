// @vitest-environment node
import { readdirSync, readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

// The starter skills (spec 13.6.1 to 13.6.4): one folder each, with a SKILL.md
// whose frontmatter names the skill and describes when to use it, and reference
// files beside it. The apply service installs whatever this folder holds.
// business-metrics is optional: it is meant for squads with goals.
const skillsDir = fileURLToPath(new URL("./skills", import.meta.url));

const frontmatterOf = (content: string): Record<string, string> | null => {
  const block = /^---\n([\s\S]*?)\n---\n/.exec(content)?.[1];
  if (block === undefined) return null;
  const fields: Record<string, string> = {};
  for (const line of block.split("\n")) {
    const [, key, value] = /^([a-z-]+): (.*)$/.exec(line) ?? [];
    if (key !== undefined && value !== undefined) fields[key] = value;
  }
  return fields;
};

const skillNames = readdirSync(skillsDir, { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .map((entry) => entry.name)
  .sort();

describe("starter skills", () => {
  it("ships the four skills: three for the starter company and one for squads with goals", () => {
    expect(skillNames).toEqual([
      "business-metrics",
      "directive-execution",
      "secretary-analysis",
      "squad-reporting",
    ]);
  });

  it.each(skillNames)("%s has a SKILL.md with name and description frontmatter", (name) => {
    const content = readFileSync(resolve(skillsDir, name, "SKILL.md"), "utf8");
    const fields = frontmatterOf(content);

    expect(fields, "frontmatter block").not.toBeNull();
    expect(fields?.name).toBe(name);
    expect(fields?.description, "description").toMatch(/^".+"$/);
    expect(fields?.description?.length).toBeGreaterThan(40);
    expect(content.slice(content.indexOf("\n---\n") + 5).trim().length).toBeGreaterThan(200);
  });

  it.each(skillNames)("%s has a non-empty reference file", (name) => {
    const path = resolve(skillsDir, name, "reference.md");

    expect(existsSync(path)).toBe(true);
    expect(readFileSync(path, "utf8").trim().length).toBeGreaterThan(200);
  });

  it("keeps the frontmatter description free of unescaped double quotes", () => {
    for (const name of skillNames) {
      const description = frontmatterOf(readFileSync(resolve(skillsDir, name, "SKILL.md"), "utf8"))
        ?.description;
      expect(description?.slice(1, -1), name).not.toContain('"');
    }
  });
});
