import { render } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { CodeBlockStatic } from "./code-block-static";

describe("CodeBlockStatic", () => {
  it("uses the standalone rich-text-editor pre shape covered by code.css", () => {
    const { container } = render(
      <CodeBlockStatic language="bash" body="uv run --extra dev pytest -q" />,
    );

    const pre = container.querySelector("pre.rich-text-editor");
    const code = container.querySelector("pre.rich-text-editor code");

    expect(pre).not.toBeNull();
    expect(code?.textContent).toBe("uv run --extra dev pytest -q");
  });

  it("keeps standalone static code blocks under the block-code CSS selectors", () => {
    const codeCss = readFileSync("editor/styles/code.css", "utf8");

    expect(codeCss).toContain("pre.rich-text-editor");
    expect(codeCss).toContain("pre.rich-text-editor code");
  });
});
