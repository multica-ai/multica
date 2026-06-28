import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(__dirname, "markdown.tsx"), "utf8");

describe("Markdown build boundary", () => {
  it("imports attachment rendering without pulling the full editor barrel", () => {
    expect(source).not.toContain('from "../editor"');
    expect(source).toContain('from "../editor/attachment"');
    expect(source).toContain('from "../editor/attachment-download-context"');
  });
});
