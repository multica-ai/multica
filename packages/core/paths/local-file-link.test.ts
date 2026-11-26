// @vitest-environment node
import { describe, expect, it } from "vitest";
import { isLocalFileHref } from "./local-file-link";

describe("isLocalFileHref", () => {
  it.each([
    "/Users/example/project/openspec/changes/example/proposal.md",
    "./docs/proposal.md",
    "../proposal.md",
    "docs/proposal.md",
    "proposal.md",
    "/home/example/My%20Project/notes.markdown#summary",
    "/home/example/%E6%96%87%E6%A1%A3.md",
    "/tmp/proposal%2Emd",
    "/tmp/invalid%ZZ.md",
    "/tmp/script.sh",
    "/tmp/code.py:42",
    "C:\\project\\proposal.md",
  ])("intercepts file-looking href %s for native validation", (href) => {
    expect(isLocalFileHref(href)).toBe(true);
  });

  it.each([
    "https://example.com/proposal.md",
    "http://localhost:3000/uploads/notes.md",
    "//example.com/proposal.md",
    "/\\example.com/proposal.md",
    "file:///tmp/proposal.md",
    "javascript:alert(1)",
    "data:text/plain,proposal.md",
    "mailto:example@example.com",
    "mention://issue/ID",
    "/acme/issues/MUL-1",
    "/issues/MUL-1",
    "#summary",
    "",
  ])("preserves URLs and application routes: %s", (href) => {
    expect(isLocalFileHref(href)).toBe(false);
  });
});
