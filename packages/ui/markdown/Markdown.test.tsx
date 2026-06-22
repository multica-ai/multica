import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Markdown } from "./Markdown";

vi.mock("react-i18next", () => ({
  useTranslation: () => ({
    t: (sel: (s: Record<string, string>) => string) => {
      const keys: Record<string, string> = {
        plain_text: "Plain text",
        copy_code: "Copy code",
        copied: "Copied",
        delete_block: "Delete block",
      };
      try {
        return sel(keys);
      } catch {
        return "Plain text";
      }
    },
  }),
}));

describe("Markdown sanitize schema", () => {
  afterEach(() => {
    cleanup();
  });

  it("renders <mark> tags from raw HTML without stripping them", () => {
    const { container } = render(
      <Markdown mode="minimal">{`This is <mark>highlighted</mark> text`}</Markdown>,
    );

    const markEl = container.querySelector("mark");
    expect(markEl).not.toBeNull();
    expect(markEl?.textContent).toBe("highlighted");
  });

  it("does not render disallowed HTML tags", () => {
    const { container } = render(
      <Markdown mode="minimal">{`This is <script>alert('xss')</script> text`}</Markdown>,
    );

    const scriptEl = container.querySelector("script");
    expect(scriptEl).toBeNull();
  });
});

describe("Markdown editor-parity mode", () => {
  afterEach(() => {
    cleanup();
  });

  it("uses .rich-text-editor.readonly CSS scope", () => {
    const { container } = render(
      <Markdown mode="editor-parity">{"Hello world"}</Markdown>,
    );

    const wrapper = container.firstElementChild;
    expect(wrapper?.className).toContain("rich-text-editor");
    expect(wrapper?.className).toContain("readonly");
  });

  it("applies postprocess hook for ==mark== highlight syntax", () => {
    // The actual highlightToHtml transform lives in views/editor/utils/.
    // Here we test that the postprocess hook is invoked by the Markdown engine.
    const postprocess = (md: string) => md.replace(/==([^=]+)==/g, "<mark>$1</mark>");
    const { container } = render(
      <Markdown mode="editor-parity" postprocess={postprocess}>
        {"This is ==highlighted== text"}
      </Markdown>,
    );

    const markEl = container.querySelector("mark");
    expect(markEl).not.toBeNull();
    expect(markEl?.textContent).toBe("highlighted");
  });

  it("renders code blocks with lowlight highlighting", async () => {
    const { container } = render(
      <Markdown mode="editor-parity">{"```javascript\nconst x = 1;\n```"}</Markdown>,
    );

    // The lowlight chunk loads async; once ready the plain-text fallback
    // upgrades to highlighted spans (e.g. .hljs-keyword for `const`).
    await waitFor(() => {
      const keyword = container.querySelector("code.hljs .hljs-keyword");
      expect(keyword).not.toBeNull();
    });
    const codeEl = container.querySelector("code.hljs");
    expect(codeEl?.className).toContain("language-javascript");
  });

  it("renders code block text before the lowlight chunk loads (graceful fallback)", () => {
    // On first render the highlighter is still null, so the block must show
    // its raw text content — never blank — until highlighting upgrades it.
    const { container } = render(
      <Markdown mode="editor-parity">{"```javascript\nconst x = 1;\n```"}</Markdown>,
    );

    const codeEl = container.querySelector("code.hljs");
    expect(codeEl).not.toBeNull();
    expect(codeEl?.textContent).toContain("const x = 1");
  });

  it("renders CodeBlockHeader with language label", () => {
    const { container } = render(
      <Markdown mode="editor-parity">{"```python\nprint('hello')\n```"}</Markdown>,
    );

    const header = container.querySelector(".code-block-header");
    expect(header).not.toBeNull();
    expect(header?.textContent).toContain("python");
  });

  it("renders CodeBlockHeader with copy button", () => {
    const { container } = render(
      <Markdown mode="editor-parity">{"```text\nhello\n```"}</Markdown>,
    );

    const copyButton = container.querySelector('button[aria-label="Copy code"]');
    expect(copyButton).not.toBeNull();
  });

  it("wraps tables in tableWrapper div", () => {
    const { container } = render(
      <Markdown mode="editor-parity">{"| a | b |\n|---|---|\n| 1 | 2 |"}</Markdown>,
    );

    const tableWrapper = container.querySelector(".tableWrapper");
    expect(tableWrapper).not.toBeNull();
    expect(tableWrapper?.querySelector("table")).not.toBeNull();
  });

  it("applies postprocess hook for bare JSON wrapping", () => {
    // The actual preprocessJsonLiterals transform lives in views/editor/utils/.
    // Here we test that the postprocess hook is invoked by the Markdown engine.
    const postprocess = (md: string) =>
      md.replace(/(\{[^}]+\})/g, "```json\n$1\n```");
    const { container } = render(
      <Markdown mode="editor-parity" postprocess={postprocess}>
        {`{"error":"not_found","status":404}`}
      </Markdown>,
    );

    const codeEl = container.querySelector("code.language-json");
    expect(codeEl).not.toBeNull();
  });

  it("delegates mermaid blocks to renderMermaid callback", () => {
    const { container } = render(
      <Markdown
        mode="editor-parity"
        renderMermaid={({ chart }) => <div data-testid="mermaid" data-chart={chart} />}
      >{"```mermaid\ngraph TD\nA-->B\n```"}</Markdown>,
    );

    const mermaidEl = container.querySelector('[data-testid="mermaid"]');
    expect(mermaidEl).not.toBeNull();
    expect(mermaidEl?.getAttribute("data-chart")).toContain("graph TD");
  });

  it("delegates html blocks to renderHtmlBlock callback", () => {
    const { container } = render(
      <Markdown
        mode="editor-parity"
        renderHtmlBlock={({ html }) => <div data-testid="html-preview" data-html={html} />}
      >{"```html\n<div>test</div>\n```"}</Markdown>,
    );

    const htmlEl = container.querySelector('[data-testid="html-preview"]');
    expect(htmlEl).not.toBeNull();
    expect(htmlEl?.getAttribute("data-html")).toContain("<div>test</div>");
  });

  it("does not run preprocessJsonLiterals in minimal mode", () => {
    const { container } = render(
      <Markdown mode="minimal">{`{"error":"not_found","status":404}`}</Markdown>,
    );

    // In minimal mode, bare JSON should NOT be wrapped in code blocks
    const codeEl = container.querySelector("code.language-json");
    expect(codeEl).toBeNull();
  });
});
