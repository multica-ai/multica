// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { TOOLS } from "./provider-picker-tools";

// The "Try it" filter on the providers page renders TOOLS, while the
// "Supported tools" table below it is written by hand in every locale. Nothing
// else ties the two together, so a tool added to the table never reached the
// filter. Table rows are the ones whose second cell (the detected command)
// starts with a backtick. Columns 3–5 are Session resumption, Multica-managed
// MCP and Model picked in Multica: "—" means no, anything else (✓, a localized
// "Mode") means yes.

const docsDir = path.resolve(__dirname, "../../content/docs");
const providerPages = readdirSync(docsDir).filter((name) => /^providers(\.[\w-]+)?\.mdx$/.test(name));

type Row = (typeof TOOLS)[number];

function parseSupportedTools(source: string): Row[] {
  return source.split("\n").flatMap((line) => {
    if (!line.startsWith("|")) return [];
    const [name, command, resume, mcp, model] = line.split("|").slice(1, -1).map((cell) => cell.trim());
    if (name === undefined || !command?.startsWith("`") || model === undefined) return [];
    const yes = (cell: string | undefined) => cell !== "—";
    return [{ name, resume: yes(resume), mcp: yes(mcp), model: yes(model) }];
  });
}

describe("provider picker tools", () => {
  it("finds the providers page in every locale", () => {
    expect(providerPages).toContain("providers.mdx");
    expect(providerPages.length).toBeGreaterThan(1);
  });

  it.each(providerPages)("matches the Supported tools table in %s", (page) => {
    const rows = parseSupportedTools(readFileSync(path.join(docsDir, page), "utf8"));
    expect(rows.length).toBeGreaterThan(0);
    expect(TOOLS).toEqual(rows);
  });
});
