import { useEffect, useRef } from "react";
import { render, screen, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Editor } from "@tiptap/react";
import { CerebroBlockHandle } from "./cerebro-block-handle";

// The DragHandle mock fires onNodeChange once with the node this holder carries,
// so a test can control which block the menu is targeting.
const nodeHolder = vi.hoisted(() => ({
  current: { type: { name: "paragraph" }, textContent: "hi" } as {
    type: { name: string };
    textContent: string;
  },
}));

vi.mock("@tiptap/extension-drag-handle-react", () => ({
  DragHandle: ({
    children,
    onNodeChange,
  }: {
    children: React.ReactNode;
    onNodeChange?: (data: { node: unknown; pos: number }) => void;
  }) => {
    // Fire strictly once. The real component passes a fresh inline onNodeChange
    // each render, so a ref guard (not a deps array) is what stops the loop.
    const fired = useRef(false);
    useEffect(() => {
      if (fired.current) return;
      fired.current = true;
      onNodeChange?.({ node: nodeHolder.current, pos: 0 });
    });
    return <div>{children}</div>;
  },
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateIssue: () => ({ mutateAsync: vi.fn() }),
}));

// Render the menu inline so items are queryable without opening a portal.
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ children }: { children: React.ReactNode }) => <button type="button">{children}</button>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div role="menu">{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" role="menuitem" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => <hr />,
  DropdownMenuSub: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuSubTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuSubContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

const editor = {} as Editor;

afterEach(cleanup);

describe("CerebroBlockHandle image guard", () => {
  it("hides Turn into / Indent / Outdent on an image block", () => {
    nodeHolder.current = { type: { name: "image" }, textContent: "" };
    render(<CerebroBlockHandle editor={editor} />);
    expect(screen.queryByText("Turn into")).not.toBeInTheDocument();
    expect(screen.queryByText("Indent")).not.toBeInTheDocument();
    expect(screen.queryByText("Outdent")).not.toBeInTheDocument();
    // Structure-agnostic actions stay available.
    expect(screen.getByText("Duplicate")).toBeInTheDocument();
    expect(screen.getByText("Delete")).toBeInTheDocument();
  });

  it("hides them on an image caption block too", () => {
    nodeHolder.current = { type: { name: "imageCaption" }, textContent: "A caption" };
    render(<CerebroBlockHandle editor={editor} />);
    expect(screen.queryByText("Turn into")).not.toBeInTheDocument();
    expect(screen.queryByText("Indent")).not.toBeInTheDocument();
  });

  it("keeps the full menu for a text block", () => {
    nodeHolder.current = { type: { name: "paragraph" }, textContent: "hi" };
    render(<CerebroBlockHandle editor={editor} />);
    expect(screen.getByText("Turn into")).toBeInTheDocument();
    expect(screen.getByText("Indent")).toBeInTheDocument();
    expect(screen.getByText("Outdent")).toBeInTheDocument();
  });
});
