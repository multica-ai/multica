import { describe, it, expect } from "vitest";
import { render, type RenderResult } from "@testing-library/react";
import { Markdown } from "@multica/ui/markdown";

// ---------------------------------------------------------------------------
// DOM probe: where does `### Heading` land for different source-content shapes?
//
// This test pins down which Markdown DOM path produces the visual misalignment
// captured in the issue screenshot. Run with:
//   pnpm --filter @multica/views exec vitest run skills/components/file-viewer.h3-dom
// ---------------------------------------------------------------------------

const TRIPLE_BACKTICK_FENCE = [
  "# Review Output Template",
  "",
  "```markdown",
  "## Review Summary",
  "",
  "**Verdict:** approve / request-changes / reject",
  "**Overview:** one-paragraph summary",
  "",
  "### Critical Issues",
  "- [File:line] description",
  "",
  "### Important Issues",
  "- [File:line] description",
  "",
  "### Suggestions",
  "- [File:line] description",
  "",
  "### What's Done Well",
  "- short note",
  "```",
].join("\n");

const MISSING_CLOSING_FENCE = [
  "# Review Output Template",
  "",
  "```markdown",
  "## Review Summary",
  "",
  "### Critical Issues",
  "- [File:line] description",
  "",
  "### Important Issues",
  "- [File:line] description",
  "",
  "### Suggestions",
  "- [File:line] description",
].join("\n");

const INDENTED_CODE_BLOCK = [
  "# Review Output Template",
  "",
  "    ## Review Summary",
  "",
  "    ### Critical Issues",
  "    - [File:line] description",
  "",
  "    ### Important Issues",
  "    - [File:line] description",
  "",
  "    ### Suggestions",
  "    - [File:line] description",
].join("\n");

const NO_FENCE_AT_ALL = [
  "# Review Output Template",
  "",
  "## Review Summary",
  "",
  "### Critical Issues",
  "- [File:line] description",
  "",
  "### Important Issues",
  "- [File:line] description",
  "",
  "### Suggestions",
  "- [File:line] description",
].join("\n");

const PARTIAL_FENCE = [
  "# Review Output Template",
  "",
  "```markdown",
  "## Review Summary",
  "```",
  "",
  "### Critical Issues",
  "- [File:line] description",
  "",
  "### Important Issues",
  "- [File:line] description",
  "",
  "### Suggestions",
  "- [File:line] description",
].join("\n");

interface Classification {
  insideShikiLine: boolean;
  standaloneH3: boolean;
  ancestorChain: string;
}

function classifyHeading(
  result: RenderResult,
  text: string,
): Classification | null {
  const candidates = Array.from(result.container.querySelectorAll("*"));
  const node = candidates.find(
    (el) => el.textContent?.trim() === text.trim() && el.children.length === 0,
  );
  if (!node) return null;

  let insideShikiLine = false;
  let standaloneH3 = false;
  const chain: string[] = [];
  let cursor: Element | null = node;
  while (cursor) {
    const tag = cursor.tagName.toLowerCase();
    const cls = cursor.getAttribute("class") ?? "";
    chain.push(cls ? `${tag}.${cls.split(/\s+/).join(".")}` : tag);
    if (
      tag === "span" &&
      cls.split(/\s+/).includes("line")
    ) {
      insideShikiLine = true;
    }
    if (
      tag === "h3" &&
      cls.split(/\s+/).includes("font-sans")
    ) {
      standaloneH3 = true;
    }
    cursor = cursor.parentElement;
  }
  return {
    insideShikiLine,
    standaloneH3,
    ancestorChain: chain.join(" < "),
  };
}

describe("FileViewer h3 DOM probe — where does `### Heading` land?", () => {
  it("triple-backtick fence keeps every ### inside the code block", () => {
    const result = render(<Markdown mode="full">{TRIPLE_BACKTICK_FENCE}</Markdown>);
    // Shiki is async; even without highlight, the fallback <pre><code> is rendered.
    // We assert there is NO standalone <h3 class="font-sans"> for the headings inside the fence.
    const h3s = Array.from(
      result.container.querySelectorAll("h3.font-sans"),
    ).filter((el) =>
      ["Critical Issues", "Important Issues", "Suggestions", "What's Done Well"].includes(
        (el.textContent ?? "").trim(),
      ),
    );
    expect(h3s).toHaveLength(0);
  });

  it("missing closing fence still keeps ### inside the code block", () => {
    const result = render(<Markdown mode="full">{MISSING_CLOSING_FENCE}</Markdown>);
    const h3s = Array.from(
      result.container.querySelectorAll("h3.font-sans"),
    ).filter((el) =>
      ["Critical Issues", "Important Issues", "Suggestions"].includes(
        (el.textContent ?? "").trim(),
      ),
    );
    expect(h3s).toHaveLength(0);
  });

  it("4-space indented code block keeps ### inside the code block", () => {
    const result = render(<Markdown mode="full">{INDENTED_CODE_BLOCK}</Markdown>);
    const h3s = Array.from(
      result.container.querySelectorAll("h3.font-sans"),
    ).filter((el) =>
      ["Critical Issues", "Important Issues", "Suggestions"].includes(
        (el.textContent ?? "").trim(),
      ),
    );
    expect(h3s).toHaveLength(0);
  });

  it("no fence at all promotes every ### to a standalone <h3>", () => {
    const result = render(<Markdown mode="full">{NO_FENCE_AT_ALL}</Markdown>);
    const c = classifyHeading(result, "Critical Issues");
    expect(c).not.toBeNull();
    expect(c!.insideShikiLine).toBe(false);
    expect(c!.standaloneH3).toBe(true);

    const standaloneH3s = Array.from(
      result.container.querySelectorAll("h3.font-sans"),
    ).map((el) => (el.textContent ?? "").trim());
    expect(standaloneH3s).toEqual(
      expect.arrayContaining(["Critical Issues", "Important Issues", "Suggestions"]),
    );
  });

  it("partial fence (only ## in fence, ### outside) promotes ### to standalone <h3>", () => {
    const result = render(<Markdown mode="full">{PARTIAL_FENCE}</Markdown>);
    const c = classifyHeading(result, "Critical Issues");
    expect(c).not.toBeNull();
    expect(c!.standaloneH3).toBe(true);
    expect(c!.insideShikiLine).toBe(false);
  });
});
