import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";

const pushSpy = vi.fn();
const openInNewTabSpy = vi.fn();
const navigationState: {
  push: typeof pushSpy;
  replace: ReturnType<typeof vi.fn>;
  back: ReturnType<typeof vi.fn>;
  pathname: string;
  searchParams: URLSearchParams;
  openInNewTab?: typeof openInNewTabSpy;
} = {
  push: pushSpy,
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/test/issues/current",
  searchParams: new URLSearchParams(),
  openInNewTab: openInNewTabSpy,
};

vi.mock("@tiptap/react", () => ({
  NodeViewWrapper: ({
    children,
    className,
  }: {
    children: ReactNode;
    className?: string;
  }) => <span className={className}>{children}</span>,
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test/issues/${id}`,
  }),
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => navigationState,
}));

vi.mock("../../issues/components/issue-chip", () => ({
  IssueChip: ({ fallbackLabel }: { fallbackLabel?: string }) => (
    <span>{fallbackLabel ?? "issue"}</span>
  ),
}));

import { MentionView } from "./mention-view";

function renderIssueMention() {
  return render(
    <MentionView
      {...({
        node: { attrs: { type: "issue", id: "issue-1", label: "MUL-1" } },
      } as unknown as Parameters<typeof MentionView>[0])}
    />,
  );
}

describe("MentionView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    navigationState.openInNewTab = openInNewTabSpy;
  });

  it("pushes the issue route on a regular click", () => {
    renderIssueMention();

    fireEvent.click(screen.getByRole("link", { name: "MUL-1" }));

    expect(pushSpy).toHaveBeenCalledWith("/test/issues/issue-1");
    expect(openInNewTabSpy).not.toHaveBeenCalled();
  });

  it("uses the navigation adapter for modifier-clicks when available", () => {
    renderIssueMention();

    fireEvent.click(screen.getByRole("link", { name: "MUL-1" }), {
      metaKey: true,
    });

    expect(openInNewTabSpy).toHaveBeenCalledWith("/test/issues/issue-1", "MUL-1");
    expect(pushSpy).not.toHaveBeenCalled();
  });

  it("falls back to window.open for web modifier-clicks without openInNewTab", () => {
    const windowOpenSpy = vi.spyOn(window, "open").mockImplementation(() => null);
    navigationState.openInNewTab = undefined;

    renderIssueMention();

    fireEvent.click(screen.getByRole("link", { name: "MUL-1" }), {
      metaKey: true,
    });

    expect(windowOpenSpy).toHaveBeenCalledWith(
      "/test/issues/issue-1",
      "_blank",
      "noopener,noreferrer",
    );
    expect(pushSpy).not.toHaveBeenCalled();
  });
});
