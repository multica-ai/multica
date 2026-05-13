import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

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
import { configStore } from "@multica/core/config";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enEditor from "../locales/en/editor.json";
import { ReadonlyContent } from "./readonly-content";

const TEST_RESOURCES = { en: { common: enCommon, editor: enEditor } };
const previewAttachmentMarkdown = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    previewAttachmentMarkdown,
  },
}));

beforeEach(() => {
  vi.clearAllMocks();
  configStore.getState().setCdnDomain("cdn.example.com");
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

describe("ReadonlyContent file cards", () => {
  it("previews markdown file cards before the download action", async () => {
    previewAttachmentMarkdown.mockResolvedValue("# Preview title\n\nGenerated markdown body");

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ReadonlyContent content="!file[permission-config-design.md](https://cdn.example.com/permission-config-design.md)" />
      </I18nProvider>,
    );

    const previewButton = screen.getByRole("button", {
      name: "Preview permission-config-design.md",
    });
    const downloadButton = screen.getByRole("button", {
      name: "Download",
    });
    expect(previewButton.compareDocumentPosition(downloadButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    fireEvent.click(previewButton);

    await waitFor(() =>
      expect(previewAttachmentMarkdown).toHaveBeenCalledWith("https://cdn.example.com/permission-config-design.md"),
    );
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("Generated markdown body");
    expect(screen.getByTestId("markdown-preview-shell")).toBeInTheDocument();
    expect(screen.getByTestId("markdown-preview-drag-handle")).toHaveTextContent(
      "permission-config-design.md",
    );
    expect(screen.getByTestId("markdown-preview-scroll")).toHaveClass("overflow-y-auto");
    expect(
      screen.getByRole("button", { name: "Enter full screen" }),
    ).toBeInTheDocument();
  });

  it("toggles markdown previews between windowed and full-screen modes", async () => {
    previewAttachmentMarkdown.mockResolvedValue("# Preview title");

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ReadonlyContent content="!file[fullscreen.md](https://cdn.example.com/fullscreen.md)" />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Preview fullscreen.md" }));

    await waitFor(() =>
      expect(previewAttachmentMarkdown).toHaveBeenCalledWith(
        "https://cdn.example.com/fullscreen.md",
      ),
    );

    expect(await screen.findByTestId("markdown-preview-shell")).toHaveAttribute("data-fullscreen", "false");

    fireEvent.click(screen.getByRole("button", { name: "Enter full screen" }));
    expect(await screen.findByTestId("markdown-preview-shell")).toHaveAttribute("data-fullscreen", "true");
    expect(
      screen.getByRole("button", { name: "Exit full screen" }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Exit full screen" }));
    expect(await screen.findByTestId("markdown-preview-shell")).toHaveAttribute("data-fullscreen", "false");
    expect(
      screen.getByRole("button", { name: "Enter full screen" }),
    ).toBeInTheDocument();
  });

  it("keeps full-screen markdown preview controls clickable in desktop drag regions", async () => {
    previewAttachmentMarkdown.mockResolvedValue("# Preview title");

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ReadonlyContent content="!file[desktop-controls.md](https://cdn.example.com/desktop-controls.md)" />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Preview desktop-controls.md" }));

    await waitFor(() =>
      expect(previewAttachmentMarkdown).toHaveBeenCalledWith(
        "https://cdn.example.com/desktop-controls.md",
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Enter full screen" }));

    expect(screen.getByRole("button", { name: "Exit full screen" })).toHaveClass(
      "[-webkit-app-region:no-drag]",
    );
    expect(screen.getByRole("button", { name: "Close preview" })).toHaveClass(
      "[-webkit-app-region:no-drag]",
    );
  });

  it("fills narrow viewports without overflowing in full-screen mode", async () => {
    previewAttachmentMarkdown.mockResolvedValue("# Preview title");
    vi.spyOn(window, "innerWidth", "get").mockReturnValue(320);
    vi.spyOn(window, "innerHeight", "get").mockReturnValue(480);

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ReadonlyContent content="!file[fullscreen-narrow.md](https://cdn.example.com/fullscreen-narrow.md)" />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Preview fullscreen-narrow.md" }));

    await waitFor(() =>
      expect(previewAttachmentMarkdown).toHaveBeenCalledWith(
        "https://cdn.example.com/fullscreen-narrow.md",
      ),
    );

    fireEvent.click(screen.getByRole("button", { name: "Enter full screen" }));

    const shell = await screen.findByTestId("markdown-preview-shell");
    expect(shell).toHaveAttribute("data-fullscreen", "true");
    expect(shell).toHaveStyle({ width: "320px", height: "480px" });
    expect(shell.style.transform.replace(/\s/g, "")).toContain("translate(0px,0px)");
  });

  it("resets markdown preview chrome after closing", async () => {
    previewAttachmentMarkdown.mockResolvedValue("# Preview title");

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ReadonlyContent content="!file[reset.md](https://cdn.example.com/reset.md)" />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Preview reset.md" }));

    fireEvent.click(screen.getByRole("button", { name: "Enter full screen" }));
    const shell = await screen.findByTestId("markdown-preview-shell");
    expect(shell).toHaveAttribute("data-fullscreen", "true");

    fireEvent.click(screen.getByRole("button", { name: "Close preview" }));
    fireEvent.click(screen.getByRole("button", { name: "Preview reset.md" }));

    expect(await screen.findByTestId("markdown-preview-shell")).toHaveAttribute("data-fullscreen", "false");
    expect(screen.getByRole("button", { name: "Enter full screen" })).toBeInTheDocument();
  });

  it("keeps initial markdown previews inside narrow viewports", async () => {
    previewAttachmentMarkdown.mockResolvedValue("# Preview title");
    vi.spyOn(window, "innerWidth", "get").mockReturnValue(390);
    vi.spyOn(window, "innerHeight", "get").mockReturnValue(520);

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ReadonlyContent content="!file[narrow.md](https://cdn.example.com/narrow.md)" />
      </I18nProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Preview narrow.md" }));

    await waitFor(() =>
      expect(previewAttachmentMarkdown).toHaveBeenCalledWith("https://cdn.example.com/narrow.md"),
    );

    const shell = await screen.findByTestId("markdown-preview-shell");
    expect(shell).toHaveStyle({ width: "358px" });
  });

  it("previews relative markdown file cards", async () => {
    previewAttachmentMarkdown.mockResolvedValue("# Local Preview");

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ReadonlyContent content="!file[local-preview.md](/uploads/workspaces/ws-1/local-preview.md)" />
      </I18nProvider>,
    );

    const previewButton = screen.getByRole("button", {
      name: "Preview local-preview.md",
    });
    fireEvent.click(previewButton);

    await waitFor(() =>
      expect(previewAttachmentMarkdown).toHaveBeenCalledWith("/uploads/workspaces/ws-1/local-preview.md"),
    );
    expect(await screen.findByRole("dialog")).toHaveTextContent("Local Preview");
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
