import type { ReactNode } from "react";
import { beforeEach, describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

const mockSetLastMode = vi.hoisted(() => vi.fn());
const mockBeginIsolatedDraft = vi.hoisted(() => vi.fn());
const mockEndIsolatedDraft = vi.hoisted(() => vi.fn());
const mockRefetchSourceContext = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: { capture_token: "sha256:preview-token" },
    isLoading: false,
    isFetching: false,
    isError: false,
    error: null,
    refetch: mockRefetchSourceContext,
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/issues/queries", () => ({
  sourceContextPreviewOptions: (wsId: string, anchorCommentId: string) => ({
    queryKey: ["source-context", "preview", wsId, anchorCommentId],
  }),
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: {
    getState: () => ({
      beginIsolatedDraft: mockBeginIsolatedDraft,
      endIsolatedDraft: mockEndIsolatedDraft,
    }),
  },
}));

const mockCreateModeStore = {
  lastMode: "agent" as "agent" | "manual",
  setLastMode: mockSetLastMode,
};

vi.mock("@multica/core/issues/stores/create-mode-store", () => ({
  useCreateModeStore: Object.assign(
    (selector: (s: typeof mockCreateModeStore) => unknown) =>
      selector(mockCreateModeStore),
    { getState: () => mockCreateModeStore },
  ),
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  DialogContent: ({
    className,
    children,
  }: {
    className?: string;
    children: ReactNode;
  }) => (
    <div data-testid="dialog-content" className={className}>
      {children}
    </div>
  ),
}));

vi.mock("./quick-create-issue", () => ({
  AgentCreatePanel: ({
    data,
    onSwitchMode,
  }: {
    data?: Record<string, unknown> | null;
    onSwitchMode?: (carry?: Record<string, unknown> | null) => void;
  }) => (
    <div>
      agent panel · {String(data?.anchor_comment_id ?? "ordinary")} · {data?.source_context_expanded ? "expanded" : "collapsed"}
      <button type="button" onClick={() => onSwitchMode?.({ parent_issue_id: data?.parent_issue_id })}>
        switch manual
      </button>
      <button
        type="button"
        onClick={() => (data?.source_context_on_expanded_change as ((expanded: boolean) => void) | undefined)?.(!data?.source_context_expanded)}
      >
        toggle source context
      </button>
    </div>
  ),
}));

vi.mock("./create-issue", () => ({
  ManualCreatePanel: ({
    data,
    onSwitchMode,
  }: {
    data?: Record<string, unknown> | null;
    onSwitchMode?: (carry?: Record<string, unknown> | null) => void;
  }) => (
    <div>
      manual panel · {String(data?.anchor_comment_id ?? "ordinary")} · {data?.source_context_expanded ? "expanded" : "collapsed"}
      <button type="button" onClick={() => onSwitchMode?.({ parent_issue_id: data?.parent_issue_id })}>
        switch agent
      </button>
      <button
        type="button"
        onClick={() => (data?.source_context_on_expanded_change as ((expanded: boolean) => void) | undefined)?.(!data?.source_context_expanded)}
      >
        toggle source context
      </button>
    </div>
  ),
  manualDialogContentClass: () => "manual-dialog-class",
}));

// `cn` is deliberately NOT mocked here: the whole point of these assertions is
// that tailwind-merge keeps the phone cap and the `sm:` width as two separate
// groups instead of collapsing them into one max-width.
import { CreateIssueDialog } from "./create-issue-dialog";

function contentClass() {
  return screen.getByTestId("dialog-content").className;
}

describe("CreateIssueDialog sizing", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });
  // MUL-6236: every width the shell sets is `!important` so it can beat
  // DialogContent's own sizing — which also beat DialogContent's
  // `max-w-[calc(100%-2rem)]` gutter, so the card ran the full width of a
  // phone screen with no margin on either side.
  it("caps the agent dialog inside the viewport on phones", () => {
    render(<CreateIssueDialog onClose={vi.fn()} initialMode="agent" />);

    expect(contentClass()).toContain("!max-w-[calc(100vw-1.5rem)]");
    expect(contentClass()).toContain("sm:!max-w-xl");
  });

  it("uses the same explicit collapsed height as manual mode", () => {
    render(<CreateIssueDialog onClose={vi.fn()} initialMode="agent" />);

    expect(contentClass()).toContain("!h-96");
    expect(contentClass()).not.toContain("!max-h-[80dvh]");
  });

  it("hands manual mode its own sizing", () => {
    render(<CreateIssueDialog onClose={vi.fn()} initialMode="manual" />);

    expect(contentClass()).toBe("manual-dialog-class");
  });

  it("leaves ordinary create outside the isolated source-context path", () => {
    render(
      <CreateIssueDialog
        onClose={vi.fn()}
        initialMode="agent"
        data={{ parent_issue_id: "ordinary-parent" }}
      />,
    );

    expect(screen.getByText(/agent panel · ordinary/)).toBeInTheDocument();
    expect(mockBeginIsolatedDraft).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "switch manual" }));
    expect(screen.getByText(/manual panel · ordinary/)).toBeInTheDocument();
  });

  it("isolates source-context drafts and preserves source identity across mode switches", () => {
    const view = render(
      <CreateIssueDialog
        onClose={vi.fn()}
        initialMode="agent"
        data={{ anchor_comment_id: "comment-source", parent_issue_id: "parent-source" }}
      />,
    );

    expect(mockBeginIsolatedDraft).toHaveBeenCalledTimes(1);
    expect(screen.getByText(/agent panel · comment-source/)).toBeInTheDocument();
    expect(contentClass()).toContain("!h-96");
    expect(contentClass()).toContain("sm:!max-w-xl");
    fireEvent.click(screen.getByRole("button", { name: "toggle source context" }));
    expect(screen.getByText(/agent panel · comment-source · expanded/)).toBeInTheDocument();
    expect(contentClass()).toContain("!h-5/6");
    expect(contentClass()).toContain("sm:!max-w-2xl");
    expect(contentClass()).not.toContain("!h-96");
    fireEvent.click(screen.getByRole("button", { name: "switch manual" }));
    expect(screen.getByText(/manual panel · comment-source · expanded/)).toBeInTheDocument();
    expect(contentClass()).toContain("manual-dialog-class");
    expect(contentClass()).toContain("!h-5/6");
    fireEvent.click(screen.getByRole("button", { name: "switch agent" }));
    expect(screen.getByText(/agent panel · comment-source/)).toBeInTheDocument();

    view.unmount();
    expect(mockEndIsolatedDraft).toHaveBeenCalledTimes(1);
  });
});
