import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enEditor from "../../locales/en/editor.json";
import { FileCardView } from "./file-card";

const TEST_RESOURCES = { en: { common: enCommon, editor: enEditor } };
const previewAttachmentMarkdown = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    previewAttachmentMarkdown,
  },
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspaceSlug: () => "test",
}));

vi.mock("@tiptap/react", () => ({
  NodeViewWrapper: ({ children, ...props }: { children: React.ReactNode }) => (
    <div {...props}>{children}</div>
  ),
  ReactNodeViewRenderer: vi.fn(),
}));

vi.mock("../readonly-content", () => ({
  ReadonlyContent: ({ content }: { content: string }) => (
    <div data-testid="readonly-content">{content}</div>
  ),
}));

function renderFileCard(attrs: Record<string, unknown>) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <FileCardView
        node={{ attrs } as never}
        // The NodeView component only reads node.attrs.
        editor={{} as never}
        getPos={() => 0}
        decorations={[]}
        selected={false}
        extension={{} as never}
        updateAttributes={vi.fn()}
        deleteNode={vi.fn()}
        view={{} as never}
        innerDecorations={{} as never}
        HTMLAttributes={{}}
      />
    </I18nProvider>,
  );
}

describe("FileCardView", () => {
  beforeEach(() => {
    previewAttachmentMarkdown.mockReset();
  });

  it("previews markdown cards before the download action", async () => {
    previewAttachmentMarkdown.mockResolvedValue("# Preview title\n\nGenerated markdown body");

    renderFileCard({
      href: "https://cdn.example.com/permission-config-design.md",
      filename: "permission-config-design.md",
      uploading: false,
    });

    const previewButton = screen.getByRole("button", {
      name: "Preview permission-config-design.md",
    });
    const downloadButton = screen.getByRole("button", {
      name: "Download permission-config-design.md",
    });
    expect(previewButton.compareDocumentPosition(downloadButton) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    fireEvent.click(previewButton);

    await waitFor(() =>
      expect(previewAttachmentMarkdown).toHaveBeenCalledWith("https://cdn.example.com/permission-config-design.md"),
    );
    expect(await screen.findByRole("dialog")).toHaveTextContent("Generated markdown body");
    expect(screen.getByTestId("markdown-preview-shell")).toBeInTheDocument();
    expect(screen.getByTestId("markdown-preview-drag-handle")).toHaveTextContent(
      "permission-config-design.md",
    );
    expect(
      screen.getByRole("button", { name: "Enter full screen" }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("markdown-preview-scroll")).toHaveClass("overflow-y-auto");
  });

  it("fetches markdown content once for a mouse click", async () => {
    previewAttachmentMarkdown.mockResolvedValue("# Preview title");

    renderFileCard({
      href: "https://cdn.example.com/permission-config-design.md",
      filename: "permission-config-design.md",
      uploading: false,
    });

    const previewButton = screen.getByRole("button", {
      name: "Preview permission-config-design.md",
    });
    fireEvent.mouseDown(previewButton);
    fireEvent.click(previewButton);

    await waitFor(() => expect(previewAttachmentMarkdown).toHaveBeenCalledTimes(1));
  });
});
