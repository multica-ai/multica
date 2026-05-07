import { describe, expect, it } from "vitest";
import { preprocessFileCards } from "./file-cards";

describe("preprocessFileCards", () => {
  it("converts root-relative local upload file cards", () => {
    const result = preprocessFileCards("!file[preview.md](/uploads/workspaces/ws-1/preview.md)", "");

    expect(result).toContain('data-type="fileCard"');
    expect(result).toContain('data-href="/uploads/workspaces/ws-1/preview.md"');
    expect(result).toContain('data-filename="preview.md"');
  });

  it("does not convert unsupported file card URL schemes", () => {
    const markdown = "!file[preview.md](javascript:alert(1))";

    expect(preprocessFileCards(markdown, "")).toBe(markdown);
  });
});
