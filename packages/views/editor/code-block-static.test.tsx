import { render } from "@testing-library/react";
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

  it("renders unlabelled code without auto-detected highlight spans", () => {
    const { container } = render(
      <CodeBlockStatic language={undefined} body="const answer = 42;" />,
    );

    const code = container.querySelector("pre.rich-text-editor code");
    expect(code?.textContent).toBe("const answer = 42;");
    expect(code?.querySelector("span")).toBeNull();
  });
});

describe("CodeBlockStatic with lineNumbers", () => {
  it("numbers every line, including blank ones", () => {
    const { container } = render(
      <CodeBlockStatic language="plaintext" body={"first\n\nthird\n"} lineNumbers />,
    );

    const lines = container.querySelectorAll(".code-line");
    expect(Array.from(lines, (line) => line.getAttribute("data-line"))).toEqual([
      "1",
      "2",
      "3",
    ]);
    expect(Array.from(lines, (line) => line.textContent)).toEqual([
      "first",
      "",
      "third",
    ]);
  });

  it("keeps a multi-line token highlighted on every line it spans", () => {
    const { container } = render(
      <CodeBlockStatic
        language="javascript"
        body={"/* one\ntwo */\nconst x = 1;"}
        lineNumbers
      />,
    );

    const lines = container.querySelectorAll(".code-line");
    expect(lines).toHaveLength(3);
    expect(lines[0]?.querySelector(".hljs-comment")?.textContent).toBe("/* one");
    expect(lines[1]?.querySelector(".hljs-comment")?.textContent).toBe("two */");
    expect(lines[2]?.querySelector(".hljs-keyword")?.textContent).toBe("const");
  });

  it("sizes the gutter to the widest line number", () => {
    const body = Array.from({ length: 120 }, (_, i) => `line ${i}`).join("\n");
    const { container } = render(
      <CodeBlockStatic language={undefined} body={body} lineNumbers />,
    );

    const pre = container.querySelector<HTMLElement>("pre.code-lines");
    expect(pre?.style.getPropertyValue("--line-digits")).toBe("3");
  });

  it("wraps long lines only when asked", () => {
    const { container, rerender } = render(
      <CodeBlockStatic language={undefined} body="a" lineNumbers />,
    );
    expect(container.querySelector("pre")).not.toHaveClass("code-lines-wrap");

    rerender(<CodeBlockStatic language={undefined} body="a" lineNumbers wrap />);
    expect(container.querySelector("pre")).toHaveClass("code-lines-wrap");
  });

  it("escapes markup in the source", () => {
    const { container } = render(
      <CodeBlockStatic language={undefined} body="<script>alert(1)</script>" lineNumbers />,
    );

    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector(".code-line")?.textContent).toBe(
      "<script>alert(1)</script>",
    );
  });
});
