// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { AGENT_INSTRUCTION_TEMPLATES } from "../agents/instruction-templates";
import {
  COMPANY_FLOOR_KEYS,
  COMPANY_FLOOR_NAMES,
  COMPANY_SQUAD_CARDS,
  COMPANY_STAGE_OWNERS,
  COMPANY_START_OPTIONS,
} from "./company-template";

// The Go apply service embeds this file. It is generated from the constants
// above by `pnpm generate:company-template`; this test fails when the two differ.
const generated = JSON.parse(
  readFileSync(
    new URL("../../../server/internal/service/company_template_data.json", import.meta.url),
    "utf8",
  ),
) as Record<string, unknown>;

describe("company template data for the server", () => {
  it("carries every squad card, unchanged", () => {
    expect(generated.squads).toEqual(COMPANY_SQUAD_CARDS);
  });

  it("carries the floors, the start options and the stage owners", () => {
    expect(generated.floor_keys).toEqual([...COMPANY_FLOOR_KEYS]);
    expect(generated.floor_names).toEqual(COMPANY_FLOOR_NAMES);
    expect(generated.start_options).toEqual([...COMPANY_START_OPTIONS]);
    expect(generated.stage_owners).toEqual(COMPANY_STAGE_OWNERS);
  });

  it("carries the starter skills, unchanged from their folders", () => {
    const skillsDir = fileURLToPath(new URL("./skills", import.meta.url));
    const folders = readdirSync(skillsDir, { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name)
      .sort();
    const expected = folders.map((name) => {
      const dir = resolve(skillsDir, name);
      const content = readFileSync(resolve(dir, "SKILL.md"), "utf8");
      const files = readdirSync(dir)
        .filter((file) => file !== "SKILL.md" && file.endsWith(".md"))
        .sort()
        .map((file) => ({ path: file, content: readFileSync(resolve(dir, file), "utf8") }));
      return {
        name,
        description: /^description: "(.*)"$/m.exec(content)?.[1],
        content,
        files,
      };
    });

    expect(generated.starter_skills).toEqual(expected);
  });

  it("carries every agent instruction template, id and text", () => {
    expect(generated.instruction_templates).toEqual(
      AGENT_INSTRUCTION_TEMPLATES.map(({ id, instructions }) => ({ id, instructions })),
    );
  });
});
