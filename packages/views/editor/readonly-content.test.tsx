import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { readFileSync } from "node:fs";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const { getAttachmentTextContentMock } = vi.hoisted(() => ({
  getAttachmentTextContentMock: vi.fn(),
}));

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
          "Inline math: $E = mc^2$",
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

  it("renders code blocks without a language tag (lowlight highlightAuto fallback)", () => {
    const token = "mul_407ec1e4464b580304362ed749f821901fd7d310";
    const { container } = render(
      <ReadonlyContent content={["```", token, "```"].join("\n")} />,
    );
    const blockCode = container.querySelector("pre code");
    expect(blockCode?.textContent?.trim()).toBe(token);
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

  it("opens a fullscreen lightbox when the toolbar button is clicked", async () => {
    const { container } = render(
      <ReadonlyContent
        content={["```mermaid", "graph LR", "  A[Start] --> B[Done]", "```"].join("\n")}
      />,
    );

    const button = await waitFor(() => {
      const found = container.querySelector<HTMLButtonElement>(
        ".mermaid-diagram-toolbar button",
      );
      expect(found).not.toBeNull();
      return found!;
    });

    expect(document.querySelector(".mermaid-diagram-lightbox")).toBeNull();

    fireEvent.click(button);

    const lightboxFrame = document.querySelector<HTMLIFrameElement>(
      ".mermaid-diagram-lightbox-frame",
    );
    expect(lightboxFrame).not.toBeNull();
    expect(lightboxFrame?.getAttribute("sandbox")).toBe("");
    expect(lightboxFrame?.srcdoc).toContain("mock diagram");
    expect(lightboxFrame?.srcdoc).toContain("max-height: 100%");

    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => {
      expect(document.querySelector(".mermaid-diagram-lightbox")).toBeNull();
    });
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
