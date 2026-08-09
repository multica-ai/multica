import { describe, it, expect, vi, beforeEach } from "vitest";
import * as React from "react";
import { render, screen, fireEvent } from "@testing-library/react";

// useIsMobile reads matchMedia, which jsdom does not implement usefully. The
// two branches of this component are chosen by that hook, so it is the mock
// point: each block below pins it to the surface it is describing.
const isMobile = vi.hoisted(() => ({ value: false }));
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => isMobile.value,
}));

import { DocumentToolsSidebar } from "./document-tools-sidebar";

const BODY = "# One\n\ntext\n\n## Two\n\nmore text\n";

function renderSidebar(references?: React.ReactNode) {
  const contentRef = React.createRef<HTMLDivElement>();
  return render(
    <div>
      <div ref={contentRef}>
        <h1>One</h1>
        <h2>Two</h2>
      </div>
      <DocumentToolsSidebar
        body={BODY}
        contentRef={contentRef}
        references={references}
      />
    </div>,
  );
}

describe("DocumentToolsSidebar — desktop edge panel (FIR-4028 slice 7)", () => {
  beforeEach(() => {
    isMobile.value = false;
  });

  it("keeps the panel closed until the edge is used, so it holds no column", () => {
    renderSidebar();
    expect(screen.queryByTestId("document-tools-panel")).not.toBeInTheDocument();
    expect(screen.getByTestId("document-tools-edge")).toBeInTheDocument();
  });

  it("opens the panel on edge hover and closes it when the pointer leaves", () => {
    renderSidebar();
    fireEvent.mouseEnter(screen.getByTestId("document-tools-edge"));
    const panel = screen.getByTestId("document-tools-panel");
    expect(panel).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: /document outline/i })).toBeInTheDocument();

    fireEvent.mouseLeave(panel);
    expect(screen.queryByTestId("document-tools-panel")).not.toBeInTheDocument();
  });

  it("toggles the panel on Cmd+Shift+O and keeps it open when the pointer leaves", () => {
    renderSidebar();
    fireEvent.keyDown(window, { key: "o", metaKey: true, shiftKey: true });
    const panel = screen.getByTestId("document-tools-panel");
    expect(panel).toBeInTheDocument();

    // Pinned by the shortcut: a stray pointer exit must not close it.
    fireEvent.mouseLeave(panel);
    expect(screen.getByTestId("document-tools-panel")).toBeInTheDocument();

    fireEvent.keyDown(window, { key: "o", metaKey: true, shiftKey: true });
    expect(screen.queryByTestId("document-tools-panel")).not.toBeInTheDocument();
  });

  it("closes a pinned panel on Escape", () => {
    renderSidebar();
    fireEvent.keyDown(window, { key: "o", metaKey: true, shiftKey: true });
    expect(screen.getByTestId("document-tools-panel")).toBeInTheDocument();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByTestId("document-tools-panel")).not.toBeInTheDocument();
  });

  it("stays visible on a tablet, where useIsMobile is already false", () => {
    // The hook flips at 768 px. A `lg:` (1024 px) gate on the desktop branch
    // would blank both branches between the two, taking References with it.
    const { container } = renderSidebar(<div>refs</div>);
    const root = container.querySelector('[data-testid="document-tools-edge"]')
      ?.parentElement as HTMLElement;
    expect(root.className).not.toMatch(/(^|\s)hidden(\s|$)/);
    expect(root.className).not.toMatch(/lg:/);
  });

  it("renders References inside the panel as its own section", () => {
    renderSidebar(<div data-testid="references-slot">References</div>);
    fireEvent.mouseEnter(screen.getByTestId("document-tools-edge"));
    const panel = screen.getByTestId("document-tools-panel");
    expect(panel).toContainElement(screen.getByTestId("references-slot"));
  });
});

describe("DocumentToolsSidebar — phone Sheet (FIR-4028 slice 7)", () => {
  beforeEach(() => {
    isMobile.value = true;
  });

  it("reaches References through the same Sheet that holds the outline", () => {
    renderSidebar(<div data-testid="references-slot">References</div>);
    fireEvent.click(screen.getByRole("button", { name: /open outline/i }));
    expect(screen.getByTestId("references-slot")).toBeInTheDocument();
  });
});
