// @vitest-environment jsdom

import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { GitHubSplitDiff } from "./github-split-diff";
import { buildSplitDiff, collapseUnchangedRows, splitDiffFromAddedFile } from "./split-diff-model";

function renderSplit(
  items: ReturnType<typeof collapseUnchangedRows>,
  language?: string,
) {
  return render(
    <GitHubSplitDiff
      items={items}
      language={language}
      expandUnchangedLabel={(count) => `Show ${count}`}
      onExpandCollapse={() => {}}
    />,
  );
}

describe("GitHubSplitDiff syntax highlight", () => {
  it("colours Go keywords on a whole-line addition without wrapping the line in ins", () => {
    const items = splitDiffFromAddedFile("func main() {\n}\n").map((row) => ({
      type: "row" as const,
      row,
    }));
    const { container } = renderSplit(items, "go");

    expect(container.querySelector(".hljs-keyword")?.textContent).toBe("func");
    expect(container.querySelector('[data-change-diff-kind="add"] ins')).toBeNull();
    expect(container.querySelector("pre")?.classList.contains("text-foreground")).toBe(true);
  });

  it("keeps word-level ins/del on the default foreground while highlighting the rest", () => {
    const items = collapseUnchangedRows(buildSplitDiff("const old = 1;", "const next = 2;"));
    const { container } = renderSplit(items, "typescript");

    expect(container.querySelector(".hljs-keyword")?.textContent).toBe("const");
    const added = container.querySelector('[data-change-diff-kind="add"] ins');
    const removed = container.querySelector('[data-change-diff-kind="remove"] del');
    expect(added).toHaveClass("text-foreground");
    expect(removed).toHaveClass("text-foreground");
    expect(added).not.toHaveClass("text-success");
    expect(removed).not.toHaveClass("text-destructive");
  });

  it("leaves unknown languages as plain text", () => {
    const items = splitDiffFromAddedFile("func main() {}\n").map((row) => ({
      type: "row" as const,
      row,
    }));
    const { container } = renderSplit(items);

    expect(container.querySelector(".hljs-keyword")).toBeNull();
    expect(container).toHaveTextContent("func main() {}");
  });
});

describe("GitHubSplitDiff GitHub gutter", () => {
  it("tints line-number gutters and omits +/- signs", () => {
    const items = collapseUnchangedRows(buildSplitDiff("old line\n", "new line\n"));
    const { container } = render(
      <GitHubSplitDiff
        items={items}
        gutter="tint"
        expandUnchangedLabel={(count) => `Show ${count}`}
        onExpandCollapse={() => {}}
      />,
    );

    const removed = container.querySelector('[data-change-diff-kind="remove"]');
    const added = container.querySelector('[data-change-diff-kind="add"]');
    expect(removed?.querySelector("[data-change-diff-sign]")).toBeNull();
    expect(added?.querySelector("[data-change-diff-sign]")).toBeNull();
    expect(removed?.querySelector("[data-change-diff-line]")).toHaveClass("bg-destructive/20");
    expect(added?.querySelector("[data-change-diff-line]")).toHaveClass("bg-success/20");
    expect(removed?.querySelector("[data-change-diff-line]")).toHaveClass("items-start");
  });

  it("renders a light-gray arrow for each tab indent", () => {
    const items = collapseUnchangedRows(buildSplitDiff("\tkeep\n", "\tnext\n"));
    const { container } = render(
      <GitHubSplitDiff
        items={items}
        gutter="tint"
        expandUnchangedLabel={(count) => `Show ${count}`}
        onExpandCollapse={() => {}}
      />,
    );

    const arrows = container.querySelectorAll("[data-diff-tab]");
    expect(arrows.length).toBeGreaterThan(0);
    expect(arrows[0]).toHaveTextContent("→");
    expect(arrows[0]).toHaveClass("text-faint-foreground");
  });
});
