import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { readFileSync } from "node:fs";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const { getAttachmentTextContentMock, resolveIssueIdentifierMock } = vi.hoisted(
  () => ({
    getAttachmentTextContentMock: vi.fn(),
    resolveIssueIdentifierMock: vi.fn(),
  }),
);

vi.mock("../issues/hooks", () => ({
  useResolveIssueIdentifier: (identifier: string) =>
    resolveIssueIdentifierMock(identifier),
}));

// i18next is not initialized in this suite, so `t()` would resolve every label
// to "". Resolve against the real EN bundle instead — the editor tree only ever
// uses the `editor` namespace — so tests can select controls by accessible name.
vi.mock("../i18n", async () => {
  const editor = (await import("../locales/en/editor.json")).default;
  return {
    useT: () => ({ t: (select: (bundle: typeof editor) => string) => select(editor) }),
    useTimeAgo: () => "just now",
  };
});

vi.mock("@multica/core/api", () => ({
  api: { getAttachmentTextContent: getAttachmentTextContentMock },
  PreviewTooLargeError: class extends Error {},
  PreviewUnsupportedError: class extends Error {},
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
  }),
  useWorkspaceSlug: () => "test",
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
}));

vi.mock("../issues/components/issue-mention-card", () => ({
  IssueMentionCard: ({ issueId, fallbackLabel }: { issueId: string; fallbackLabel?: string }) => (
    <span data-testid="issue-mention-card">{fallbackLabel ?? issueId}</span>
  ),
}));

vi.mock("./extensions/image-view", () => ({
  ImageLightbox: () => null,
}));

vi.mock("./link-hover-card", () => ({
  useLinkHover: () => ({}),
  LinkHoverCard: () => null,
}));

vi.mock("./utils/link-handler", () => ({
  openLink: vi.fn(),
  isMentionHref: (href?: string) => Boolean(href?.startsWith("mention://")),
}));

vi.mock("mermaid", () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn().mockResolvedValue({
      svg: '<svg viewBox="0 0 123 45"><g><text>mock diagram</text></g></svg>',
    }),
  },
}));

Object.defineProperty(HTMLCanvasElement.prototype, "getContext", {
  value: () => ({
    fillStyle: "#000",
    fillRect: vi.fn(),
    getImageData: () => ({ data: new Uint8ClampedArray([12, 34, 56, 255]) }),
  }),
});

import mermaid from "mermaid";
import { ReadonlyContent } from "./readonly-content";

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ReadonlyContent memoization", () => {
  // Long-timeline issues (Inbox + IssueDetail with thousands of comments)
  // freeze the tab when each comment re-runs the full react-markdown pipeline
  // on every parent re-render. Wrapping the component in React.memo is the
  // mitigation; this test guards against a future revert that would silently
  // reintroduce the perf regression.
  it("is wrapped in React.memo", () => {
    const memoTypeSymbol = Symbol.for("react.memo");
    expect((ReadonlyContent as unknown as { $$typeof: symbol }).$$typeof).toBe(
      memoTypeSymbol,
    );
  });
});

describe("ReadonlyContent math rendering", () => {
  it("renders inline and block LaTeX with KaTeX markup", () => {
    const { container } = render(
      <ReadonlyContent
        content={[
          "Inline math: $$E = mc^2$$",
          "",
          "$$",
          "\\int_0^1 x^2 \\, dx",
          "$$",
        ].join("\n")}
      />,
    );

    const text = container.textContent?.replace(/\s+/g, " ") ?? "";
    expect(container.querySelectorAll(".katex").length).toBeGreaterThanOrEqual(2);
    expect(container.querySelector(".katex-display")).not.toBeNull();
    expect(text).toContain("E = mc^2");
    expect(text).toContain("\\int_0^1 x^2 \\, dx");
  });
});

describe("ReadonlyContent line breaks", () => {
  // Issue panel comments are the primary user-visible surface for agent
  // output. CommonMark's default soft-break behavior collapses single
  // newlines into spaces; agent text often relies on a single newline as a
  // visible break. remark-breaks must remain wired into ReadonlyContent's
  // remark plugin chain or comments lose their formatting again.
  it("converts a single newline into a <br>", () => {
    const { container } = render(<ReadonlyContent content={"line one\nline two"} />);
    expect(container.querySelector("br")).not.toBeNull();
  });

  it("renders a blank-line gap as separate paragraphs", () => {
    const { container } = render(<ReadonlyContent content={"para one\n\npara two"} />);
    expect(container.querySelectorAll("p").length).toBeGreaterThanOrEqual(2);
  });
});

describe("ReadonlyContent task lists", () => {
  it("renders `- [ ]` / `- [x]` as checkboxes and preserves the checked state", () => {
    const { container } = render(
      <ReadonlyContent content={"- [ ] todo\n- [x] done"} />,
    );

    const boxes = container.querySelectorAll<HTMLInputElement>(
      'input[type="checkbox"]',
    );
    expect(boxes).toHaveLength(2);
    // The completed item must render checked, not just present.
    expect(boxes[0]!.checked).toBe(false);
    expect(boxes[1]!.checked).toBe(true);
    // Checkboxes are display-only in readonly mode.
    expect(boxes[1]!.disabled).toBe(true);
  });

  it("nests a child task list inside its parent item (not as a sibling)", () => {
    const { container } = render(
      <ReadonlyContent content={"- [ ] parent\n  - [x] child\n  - [ ] child2"} />,
    );

    // One top-level list with a single parent item.
    const root = container.querySelector("ul.contains-task-list");
    expect(root).not.toBeNull();
    const topItems = root!.querySelectorAll(":scope > li.task-list-item");
    expect(topItems).toHaveLength(1);

    // The child list lives INSIDE the parent <li> — this is the structural
    // assumption the readonly CSS depends on (no <div> body wrapper, so the
    // parent item must stay a block, not flex, or the nested <ul> shares the
    // parent's row). If remark-gfm ever wrapped the body, this fails loudly.
    const parent = topItems[0]!;
    const nested = parent.querySelector(":scope > ul.contains-task-list");
    expect(nested).not.toBeNull();

    const childItems = nested!.querySelectorAll(":scope > li.task-list-item");
    expect(childItems).toHaveLength(2);
    const childBoxes = nested!.querySelectorAll<HTMLInputElement>(
      'input[type="checkbox"]',
    );
    expect(childBoxes[0]!.checked).toBe(true);
    expect(childBoxes[1]!.checked).toBe(false);
  });
});

describe("ReadonlyContent highlight Markdown", () => {
  // `==text==` is lowered to a raw <mark> by highlightToHtml; rehype-raw turns
  // it into an element and the sanitize schema must whitelist <mark> or it gets
  // stripped. These guard both halves of that contract.
  it("renders ==text== as a <mark> element", () => {
    const { container } = render(<ReadonlyContent content={"a ==hi== b"} />);
    const mark = container.querySelector("mark");
    expect(mark).not.toBeNull();
    expect(mark?.textContent).toBe("hi");
  });

  it("keeps inner Markdown formatting inside a highlight", () => {
    const { container } = render(<ReadonlyContent content={"==**bold**=="} />);
    expect(container.querySelector("mark strong")).not.toBeNull();
  });

  it("does not highlight == inside inline code", () => {
    const { container } = render(<ReadonlyContent content={"`a ==b== c`"} />);
    expect(container.querySelector("mark")).toBeNull();
    expect(container.querySelector("code")?.textContent).toBe("a ==b== c");
  });

  // Boundary regressions (Emacs review, PR #3661).

  it("wraps the whole span when an inner == lives in inline code", () => {
    const { container } = render(<ReadonlyContent content={"==a `b==c` d=="} />);
    const mark = container.querySelector("mark");
    expect(mark).not.toBeNull();
    // inner `==` stays inside the code, not consumed as the closing fence
    expect(mark?.querySelector("code")?.textContent).toBe("b==c");
    expect(mark?.textContent).toBe("a b==c d");
  });

  it("does not highlight across a blank line", () => {
    const { container } = render(<ReadonlyContent content={"==a\n\nb=="} />);
    expect(container.querySelector("mark")).toBeNull();
  });
});

describe("ReadonlyContent issue mention Markdown", () => {
  it("renders an issue mention inside a task list as an issue mention card", () => {
    const { container, getByTestId } = render(
      <ReadonlyContent content="- [ ] [MUL-123](mention://issue/issue-123)" />,
    );

    expect(container.querySelector('input[type="checkbox"]')).not.toBeNull();
    expect(getByTestId("issue-mention-card").textContent).toBe("MUL-123");
  });

  it("autolinks a resolved bare identifier as an issue mention card", () => {
    resolveIssueIdentifierMock.mockImplementation((id: string) =>
      id === "MUL-7" ? { id: "issue-7", identifier: "MUL-7" } : null,
    );

    const { getByTestId } = render(
      <ReadonlyContent content="See MUL-7 for context" />,
    );

    expect(getByTestId("issue-mention-card").textContent).toBe("MUL-7");
    expect(resolveIssueIdentifierMock).toHaveBeenCalledWith("MUL-7");
  });

  it("leaves an unresolved bare identifier as plain text", () => {
    resolveIssueIdentifierMock.mockReturnValue(null);

    const { container, queryByTestId } = render(
      <ReadonlyContent content="See MUL-999 for context" />,
    );

    expect(queryByTestId("issue-mention-card")).toBeNull();
    expect(container.textContent).toContain("MUL-999");
  });

  it("does not autolink a bare identifier inside inline code", () => {
    resolveIssueIdentifierMock.mockReturnValue(null);

    const { queryByTestId } = render(
      <ReadonlyContent content={"use `MUL-7` here"} />,
    );

    expect(resolveIssueIdentifierMock).not.toHaveBeenCalled();
    expect(queryByTestId("issue-mention-card")).toBeNull();
  });

  it("documents the CommonMark quoted-emphasis edge case before Korean particles", () => {
    const unsafe = render(
      <ReadonlyContent content={'**"무엇을 먼저 정해두고 시작할지"**가'} />,
    );

    expect(unsafe.container.querySelector("strong")).toBeNull();
    expect(unsafe.container.textContent).toContain(
      '**"무엇을 먼저 정해두고 시작할지"**가',
    );

    const safe = render(
      <ReadonlyContent content={'"**무엇을 먼저 정해두고 시작할지**"가'} />,
    );

    expect(safe.container.querySelector("strong")?.textContent).toBe(
      "무엇을 먼저 정해두고 시작할지",
    );
    expect(safe.container.textContent).toContain('"무엇을 먼저 정해두고 시작할지"가');
  });
});

describe("ReadonlyContent code styling", () => {
  const literalCode = "uv run --extra dev pytest -q";

  it("renders inline and fenced code through rich-text-editor code selectors", () => {
    const { container } = render(
      <ReadonlyContent
        content={[
          `<code>${literalCode}</code>`,
          "",
          "```",
          literalCode,
          "```",
        ].join("\n")}
      />,
    );

    const inlineCode = Array.from(container.querySelectorAll("code")).find(
      (code) => !code.closest("pre"),
    );
    const blockCode = container.querySelector("pre code");

    expect(inlineCode?.textContent).toBe(literalCode);
    expect(blockCode?.textContent).toBe(literalCode);
  });

  it("renders code blocks without a language tag as plaintext", () => {
    const token = "const answer = 42;";
    const { container } = render(
      <ReadonlyContent content={["```", token, "```"].join("\n")} />,
    );
    const blockCode = container.querySelector("pre code");
    expect(blockCode?.textContent?.trim()).toBe(token);
    expect(blockCode?.querySelector("span")).toBeNull();
  });

  it("copies the whole fenced code block from the readonly toolbar", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const source = ["pnpm install", "pnpm test"].join("\n");
    const { getByRole } = render(
      <ReadonlyContent content={["```bash", source, "```"].join("\n")} />,
    );

    fireEvent.click(getByRole("button", { name: "Copy code" }));

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(source);
    });
  });

  it("keeps editor code literal by disabling font ligatures", () => {
    const codeCss = readFileSync("editor/styles/code.css", "utf8");

    expect(codeCss).toContain(".rich-text-editor code");
    expect(codeCss).toContain(".rich-text-editor pre");
    expect(codeCss).toContain(".rich-text-editor pre code");
    expect(codeCss).toContain("font-variant-ligatures: none;");
    expect(codeCss).toContain('font-feature-settings: "liga" 0;');
  });
});

describe("ReadonlyContent Mermaid rendering", () => {
  it("renders mermaid code fences in a sized sandbox iframe with legacy rgb colors", async () => {
    const originalGetComputedStyle = window.getComputedStyle;
    vi.spyOn(window, "getComputedStyle").mockImplementation((element, pseudoElt) => {
      if (element instanceof HTMLElement && element.style.color.startsWith("var(")) {
        return { color: "oklch(60% 0.2 120)" } as CSSStyleDeclaration;
      }
      return originalGetComputedStyle.call(window, element, pseudoElt);
    });

    const { container } = render(
      <ReadonlyContent
        content={["```mermaid", "graph LR", "  A[Start] --> B[Done]", "```"].join("\n")}
      />,
    );

    expect(container.querySelector(".mermaid-diagram")).not.toBeNull();
    expect(container.querySelector("pre code.language-mermaid")).toBeNull();

    await waitFor(() => {
      const iframe = container.querySelector<HTMLIFrameElement>(".mermaid-diagram-frame");
      expect(iframe).not.toBeNull();
      expect(iframe?.getAttribute("sandbox")).toBe("");
      expect(iframe?.srcdoc).toContain("mock diagram");
      expect(iframe?.style.width).toBe("123px");
      expect(iframe?.style.height).toBe("45px");
    });

    expect(mermaid.initialize).toHaveBeenCalledWith(
      expect.objectContaining({
        themeVariables: expect.objectContaining({
          lineColor: "rgb(12, 34, 56)",
          primaryBorderColor: "rgb(12, 34, 56)",
          primaryColor: "rgb(12, 34, 56)",
          primaryTextColor: "rgb(12, 34, 56)",
        }),
      }),
    );
  });

  it("does not regress Mermaid unwrap after the HtmlBlockPreview branch was added", async () => {
    // Both Mermaid and HtmlBlockPreview rely on react-markdown's `code`
    // renderer returning a non-<code> React element, and on the `pre`
    // renderer recognizing the element by reference and unwrapping it. If
    // someone tightens the `pre` check to a single component, the other
    // one quietly regresses into a `<pre>`-wrapped DOM. This test pins the
    // contract.
    const { container } = render(
      <ReadonlyContent
        content={["```mermaid", "graph LR", "  A --> B", "```"].join("\n")}
      />,
    );
    expect(container.querySelector(".mermaid-diagram")).not.toBeNull();
    // No outer <pre> envelope.
    expect(container.querySelector("pre")).toBeNull();
  });

  it("opens the fullscreen viewer from the toolbar and closes it with Escape", async () => {
    const { container } = render(
      <ReadonlyContent
        content={["```mermaid", "graph LR", "  A[Start] --> B[Done]", "```"].join("\n")}
      />,
    );

    const expandButton = await waitFor(() => {
      const found = container.querySelector<HTMLButtonElement>(
        '.mermaid-diagram-toolbar button[aria-label="Open diagram viewer"]',
      );
      expect(found).not.toBeNull();
      return found!;
    });

    expect(document.querySelector(".mermaid-viewer-canvas")).toBeNull();

    fireEvent.click(expandButton);

    const viewerFrame = await waitFor(() => {
      const found = document.querySelector<HTMLIFrameElement>(".mermaid-viewer-frame");
      expect(found).not.toBeNull();
      return found!;
    });
    expect(viewerFrame.getAttribute("sandbox")).toBe("");
    expect(viewerFrame.srcdoc).toContain("mock diagram");
    // The viewer draws at natural size and lets the host transform handle zoom;
    // an inline-style max-width clamp here would cap how far it can scale.
    expect(viewerFrame.srcdoc).toContain("width: 123px");
    expect(viewerFrame.srcdoc).toContain("max-width: none");

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => {
      expect(document.querySelector(".mermaid-viewer-canvas")).toBeNull();
    });
  });

  it("keeps the inline toolbar outside the scroll container so wide diagrams stay openable", async () => {
    const { container } = render(
      <ReadonlyContent
        content={["```mermaid", "graph LR", "  A[Start] --> B[Done]", "```"].join("\n")}
      />,
    );

    await waitFor(() => {
      expect(container.querySelector(".mermaid-diagram-toolbar")).not.toBeNull();
    });

    // Previously the toolbar was an absolutely-positioned child of the
    // horizontally-scrolling element, so on a wide diagram it scrolled out of
    // view along with the content and left no way to open or copy it.
    const scroller = container.querySelector(".mermaid-diagram-scroll");
    expect(scroller).not.toBeNull();
    expect(scroller?.querySelector(".mermaid-diagram-toolbar")).toBeNull();
  });

  it("shows the compact error state instead of embedding Mermaid's parser error SVG", async () => {
    // With suppressErrorRendering enabled, invalid syntax makes render() reject
    // instead of emitting Mermaid's built-in error graphic.
    vi.mocked(mermaid.render).mockRejectedValueOnce(
      new Error("Parse error on line 3"),
    );

    const chart = "graph LR\n  A -->";
    const { container } = render(
      <ReadonlyContent content={["```mermaid", chart, "```"].join("\n")} />,
    );

    await waitFor(() => {
      expect(container.querySelector(".mermaid-diagram-error")).not.toBeNull();
    });

    expect(container.querySelector(".mermaid-diagram-frame")).toBeNull();
    expect(container.querySelector(".mermaid-diagram-error code")?.textContent).toBe(
      chart,
    );
  });
});

describe("ReadonlyContent HTML block rendering", () => {
  // `language=html` fenced blocks should default to a preview iframe with
  // sandbox="allow-scripts" (chart JS executes in an opaque origin) and
  // must NOT be wrapped by react-markdown's default <pre>, which would
  // clamp the iframe with monospace / overflow styles. The two-layer
  // code+pre unwrap mirror's Mermaid's pattern.
  it("renders an iframe with sandbox='allow-scripts' for ```html and skips the outer <pre>", () => {
    const { container } = render(
      <ReadonlyContent
        content={["```html", '<h1 id="x">hi</h1>', "```"].join("\n")}
      />,
    );
    const frame = container.querySelector<HTMLIFrameElement>("iframe");
    expect(frame).not.toBeNull();
    expect(frame?.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame?.getAttribute("srcdoc")).toContain('<h1 id="x">hi</h1>');
    expect(container.querySelector("pre")).toBeNull();
  });

  it("keeps the <pre><code> wrapper for adjacent languages like htmlbars / mermaidx", () => {
    // Regression: the previous `className.includes("language-html")` check
    // matched `language-htmlbars` too, so an htmlbars fence lost its outer
    // <pre> envelope and rendered as bare lowlight-highlighted spans. The
    // unwrap rule must match the exact class token, not a prefix.
    const { container } = render(
      <ReadonlyContent
        content={[
          "```htmlbars",
          "<div>{{name}}</div>",
          "```",
          "",
          "```mermaidx",
          "not a real lang",
          "```",
        ].join("\n")}
      />,
    );
    const pres = container.querySelectorAll("pre");
    // Both fences keep their <pre> wrapper.
    expect(pres.length).toBe(2);
    // And the inner <code> still carries the original language class.
    expect(
      container.querySelector("pre code.language-htmlbars"),
    ).not.toBeNull();
    expect(
      container.querySelector("pre code.language-mermaidx"),
    ).not.toBeNull();
  });
});

describe("ReadonlyContent file-card → AttachmentBlock HTML routing", () => {
  // Regression pin for readonly-content.tsx:279. The `div data-type=fileCard`
  // branch must render through <AttachmentBlock>, not the older
  // <AttachmentCard>. Reverting that line would skip the html+attachmentId
  // dispatcher branch and surface the bare file-card chrome (filename row)
  // instead of the rendered iframe — the exact regression MUL-2330 fixed.
  function renderWithQuery(ui: ReactElement) {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
  }

  it("renders the !file[](url) HTML attachment as an iframe (no file-card chrome)", async () => {
    getAttachmentTextContentMock.mockResolvedValueOnce({
      text: "<p>chart</p>",
      originalContentType: "text/html",
    });
    const attachment = {
      id: "att-1",
      url: "/uploads/report.html",
      filename: "report.html",
      content_type: "text/html",
      size_bytes: 0,
    } as any;
    const { container, queryByText } = renderWithQuery(
      <ReadonlyContent
        content="!file[report.html](/uploads/report.html)"
        attachments={[attachment]}
      />,
    );
    const frame = await waitFor(() => {
      const f = container.querySelector<HTMLIFrameElement>("iframe");
      expect(f).not.toBeNull();
      return f!;
    });
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(frame.getAttribute("srcdoc")).toContain("<p>chart</p>");
    // AttachmentCard chrome surfaces the filename as visible text in a
    // <p class="truncate"> row. HtmlAttachmentPreview replaces it entirely.
    expect(queryByText("report.html")).toBeNull();
  });

  it("renders a stable attachment download URL as file-card chrome", () => {
    const id = "11111111-2222-3333-4444-555555555555";
    const href = `/api/attachments/${id}/download`;
    const attachment = {
      id,
      url: "/uploads/report.pdf",
      filename: "report.pdf",
      content_type: "application/pdf",
      size_bytes: 1024,
      markdown_url: href,
      download_url: href,
    } as any;

    const { container, getByText } = renderWithQuery(
      <ReadonlyContent
        content={`!file[report.pdf](${href})`}
        attachments={[attachment]}
      />,
    );

    expect(getByText("report.pdf")).toBeTruthy();
    expect(container.querySelector("iframe")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
  });

  it("resolves a markdown image whose src is the response download_url", () => {
    const href = "https://cdn.example.test/shot.png?Signature=stale";
    const fresh = "https://cdn.example.test/shot.png?Signature=fresh";
    const attachment = {
      id: "11111111-2222-3333-4444-555555555555",
      url: "https://cdn.example.test/shot.png",
      download_url: fresh,
      markdown_url: "/api/attachments/11111111-2222-3333-4444-555555555555/download",
      filename: "shot.png",
      content_type: "image/png",
      size_bytes: 1024,
    } as any;

    const { container } = renderWithQuery(
      <ReadonlyContent
        content={`![](${href})`}
        attachments={[attachment]}
      />,
    );

    const img = container.querySelector("img");
    expect(img?.getAttribute("src")).toBe(fresh);
    expect(img?.getAttribute("alt")).toBe("shot.png");
  });
});

describe("ReadonlyContent inline data-URI images", () => {
  // Issue comments render through ReadonlyContent, which has its own sanitize
  // schema + urlTransform separate from the base Markdown component. Agents
  // inline auth QR codes as `![](data:image/png;base64,...)`; both gates used
  // to strip the src and surface a broken image (MUL-3961).
  const PNG_1X1 =
    "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

  function renderWithQuery(ui: ReactElement) {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
  }

  it("preserves the src of an inline data:image/png image", () => {
    const { container } = renderWithQuery(
      <ReadonlyContent content={`![QR Code](${PNG_1X1})`} />,
    );

    expect(container.querySelector("img")?.getAttribute("src")).toBe(PNG_1X1);
  });

  it("strips non-image data URIs (data:text/html)", () => {
    const { container } = renderWithQuery(
      <ReadonlyContent content={"![x](data:text/html,<script>alert(1)</script>)"} />,
    );

    // The value allow-list rejects non-image data URIs, so no usable src reaches
    // the <img>. AttachmentRenderer still mounts an <img>, but with an empty src.
    expect(container.querySelector("img")?.getAttribute("src") ?? "").toBe("");
  });
});

describe("ReadonlyContent slash command rendering", () => {
  it("renders slash skill links as slash command pills", () => {
    const { container } = render(
      <ReadonlyContent content="[/deploy](slash://skill/abc-123)" />,
    );

    const pill = container.querySelector(".slash-command");
    expect(pill).not.toBeNull();
    expect(pill?.textContent).toBe("/deploy");
  });

  it("does not affect regular links", () => {
    const { container } = render(
      <ReadonlyContent content="[docs](https://example.com)" />,
    );

    expect(container.querySelector(".slash-command")).toBeNull();
    expect(container.querySelector("a")).not.toBeNull();
  });
});

describe("ReadonlyContent bare URL autolinking (MUL-4242)", () => {
  // A bare URL wrapped in bold used to be linkified into [url**](url**), which
  // swallowed the closing `**`: the bold never closed (leading `**` showed as
  // literal asterisks) and the href was corrupted with a trailing `**`. The
  // shared linkify now drops a trailing markdown-delimiter run from the URL, so
  // the closing `**` stays as emphasis outside a clean [url](url).
  it("renders a bold-wrapped bare URL as bold plus a clean link", () => {
    const url = "https://github.com/multica-ai/multica/pull/5081";
    const { container } = render(<ReadonlyContent content={`**PR：${url}**`} />);

    const strong = container.querySelector("strong");
    expect(strong).not.toBeNull();
    const anchor = strong!.querySelector("a");
    expect(anchor?.getAttribute("href")).toBe(url);
    // No literal asterisks leak into the text, no trailing `**` in the href.
    expect(container.textContent).not.toContain("**");
    expect(anchor?.getAttribute("href")).not.toContain("*");
  });

  it("bolds a bare URL even when a CJK punctuation immediately follows (variant B)", () => {
    // `**url**（MUL）` — the closing `**` is glued to a fullwidth paren. gfm
    // autolink swallowed the `**` here; the shared string linkify does not.
    const url = "https://github.com/multica-ai/multica/pull/5133";
    const { container } = render(
      <ReadonlyContent content={`PR：**${url}**（MUL-4277）。`} />,
    );

    const strong = container.querySelector("strong");
    expect(strong).not.toBeNull();
    expect(strong!.querySelector("a")?.getAttribute("href")).toBe(url);
    expect(container.textContent).not.toContain("**");
    expect(container.textContent).toContain("（MUL-4277）");
  });

  it("still autolinks a plain bare URL", () => {
    const { container } = render(
      <ReadonlyContent content={"see https://example.com/foo here"} />,
    );
    expect(container.querySelector('a[href="https://example.com/foo"]')).not.toBeNull();
  });

  it("stops an autolinked URL at CJK punctuation instead of swallowing it", () => {
    const { container } = render(
      <ReadonlyContent content={"见 https://example.com/foo。后面还有字"} />,
    );
    const anchor = container.querySelector("a");
    expect(anchor?.getAttribute("href")).toBe("https://example.com/foo");
    expect(anchor?.textContent).toBe("https://example.com/foo");
    // The CJK tail stays outside the link.
    expect(container.textContent).toContain("。后面还有字");
  });

  it("keeps every URL in a CJK-separated run linked, not just the first", () => {
    // `url1、url2` — linkify-it merges both across the CJK comma; collectLinkify
    // truncates at 、 and rescans the tail so both URLs become their own link.
    const { container } = render(
      <ReadonlyContent content={"两个地址 https://a.com/x、https://b.com/y"} />,
    );
    const hrefs = Array.from(container.querySelectorAll("a")).map((a) =>
      a.getAttribute("href"),
    );
    expect(hrefs).toContain("https://a.com/x");
    expect(hrefs).toContain("https://b.com/y");
    // The 、 separator stays as text between the two links.
    expect(container.textContent).toContain("、");
  });

  it("leaves an explicit link's destination untouched even when it ends in CJK", () => {
    const { container } = render(
      <ReadonlyContent content={"[看](https://example.com/x。)后文"} />,
    );
    const anchor = container.querySelector("a");
    // react-markdown percent-encodes the CJK char; the point is it is NOT
    // trimmed off the way an autolink literal would be.
    expect(decodeURIComponent(anchor?.getAttribute("href") ?? "")).toBe(
      "https://example.com/x。",
    );
    expect(anchor?.textContent).toBe("看");
  });
});
