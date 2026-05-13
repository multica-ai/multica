import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, waitFor } from "@testing-library/react";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
  }),
  useWorkspaceSlug: () => "test",
}));

const navigation = {
  push: vi.fn(),
  openInNewTab: vi.fn(),
};

vi.mock("../navigation", () => ({
  useNavigation: () => navigation,
}));

const useIsMobileMock = vi.fn(() => false);

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => useIsMobileMock(),
}));

vi.mock("../issues/components/issue-chip", () => ({
  IssueChip: ({ issueId, fallbackLabel }: { issueId: string; fallbackLabel?: string }) => (
    <span data-testid="issue-chip">{fallbackLabel ?? issueId}</span>
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
  useIsMobileMock.mockReturnValue(false);
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

describe("ReadonlyContent issue mention chip", () => {
  // JEH-874: on desktop / wide web, the chip opens the referenced issue in a new
  // tab so the comment thread stays put. JEH-1112: on mobile the PWA is
  // `display: standalone` with no tab UI — `target="_blank"` either navigates
  // the current window (iOS) or breaks out into the system browser (Android),
  // both of which drop the thread context. Fall back to SPA push so the browser
  // back-button restores scroll + draft state via the router cache.
  const chipMarkdown = "Reference: [JEH-9](mention://issue/issue-9)";

  it("opens in a new tab when not on mobile", () => {
    useIsMobileMock.mockReturnValue(false);
    const { getByTestId } = render(<ReadonlyContent content={chipMarkdown} />);
    const chip = getByTestId("issue-chip");
    const anchor = chip.closest("a");
    expect(anchor?.getAttribute("target")).toBe("_blank");
    fireEvent.click(anchor!);
    expect(navigation.openInNewTab).toHaveBeenCalledWith(
      "/test/issues/issue-9",
      "JEH-9",
    );
    expect(navigation.push).not.toHaveBeenCalled();
  });

  it("uses SPA push (same window) when on mobile", () => {
    useIsMobileMock.mockReturnValue(true);
    const { getByTestId } = render(<ReadonlyContent content={chipMarkdown} />);
    const chip = getByTestId("issue-chip");
    const anchor = chip.closest("a");
    expect(anchor?.getAttribute("target")).toBeNull();
    expect(anchor?.getAttribute("rel")).toBeNull();
    fireEvent.click(anchor!);
    expect(navigation.push).toHaveBeenCalledWith("/test/issues/issue-9");
    expect(navigation.openInNewTab).not.toHaveBeenCalled();
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
