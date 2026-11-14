import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  workspaces: [] as { id: string; slug: string }[],
  ready: false,
  unavailable: false,
  isFetching: false,
  refetch: vi.fn(),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (auth: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/workspace", () => ({
  useWorkspaceList: () => ({
    workspaces: state.workspaces,
    ready: state.ready,
    unavailable: state.unavailable,
    isFetching: state.isFetching,
    refetch: state.refetch,
  }),
}));

vi.mock("@multica/core/platform", () => ({
  setCurrentWorkspace: vi.fn(),
}));

vi.mock("@multica/core/paths", () => ({
  WorkspaceSlugProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

vi.mock("@multica/ui/components/common/multica-icon", () => ({
  MulticaIcon: () => <div data-testid="workspace-loading" />,
}));

vi.mock("@multica/views/modals/registry", () => ({
  ModalRegistry: () => null,
}));

vi.mock("@multica/views/layout", () => ({
  WorkspacePresencePrefetch: () => null,
}));

vi.mock("@multica/views/voice", () => ({
  VoiceWorkspaceWords: () => null,
}));

vi.mock("@multica/views/platform", () => ({
  DragStrip: () => <div data-testid="drag-strip" />,
  WindowTitleBarProvider: ({
    value,
    children,
  }: {
    value: { leadingInset: number } | null;
    children: React.ReactNode;
  }) => (
    <div data-testid="title-bar-scope" data-leading-inset={value?.leadingInset ?? ""}>
      {children}
    </div>
  ),
}));

vi.mock("../pages/issue-detail-page", () => ({
  IssueDetailPage: () => <div data-testid="issue-detail" />,
}));

vi.mock("../pages/auth-recovery", () => ({
  DesktopAuthRecoveryPage: ({
    onRetry,
    isRetrying,
  }: {
    onRetry: () => void;
    isRetrying: boolean;
  }) => (
    <button type="button" disabled={isRetrying} onClick={onRetry}>
      Retry workspace list
    </button>
  ),
}));

vi.mock("../platform/issue-window-navigation", () => ({
  IssueWindowNavigationProvider: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
}));

import { IssueWindow } from "./issue-window";

const context = {
  kind: "issue" as const,
  path: "/acme/issues/MUL-1",
  title: "MUL-1",
  workspaceSlug: "acme",
  issueId: "MUL-1",
};

beforeEach(() => {
  state.workspaces = [];
  state.ready = false;
  state.unavailable = false;
  state.isFetching = false;
  state.refetch.mockReset();
});

describe("IssueWindow", () => {
  it("keeps loading after an initial workspace-list failure", async () => {
    render(<IssueWindow context={context} />);

    expect(await screen.findByTestId("workspace-loading")).toBeInTheDocument();
    expect(screen.queryByText("Issue unavailable")).toBeNull();
  });

  it("shows unavailable only after an authoritative list omits the workspace", async () => {
    state.ready = true;

    render(<IssueWindow context={context} />);

    expect(await screen.findByText("Issue unavailable")).toBeInTheDocument();
    expect(screen.queryByTestId("workspace-loading")).toBeNull();
  });

  it("offers a retry when the initial workspace-list request fails", async () => {
    state.unavailable = true;

    render(<IssueWindow context={context} />);

    const retry = await screen.findByRole("button", {
      name: "Retry workspace list",
    });
    fireEvent.click(retry);
    expect(state.refetch).toHaveBeenCalledOnce();
    expect(screen.queryByTestId("workspace-loading")).toBeNull();
  });

  it("lets the issue header be the title bar instead of stacking a drag strip over it", async () => {
    state.ready = true;
    state.workspaces = [{ id: "ws-1", slug: "acme" }];

    render(<IssueWindow context={context} />);

    expect(await screen.findByTestId("issue-detail")).toBeInTheDocument();
    expect(screen.queryByTestId("drag-strip")).toBeNull();
    expect(screen.getByTestId("title-bar-scope")).toHaveAttribute("data-leading-inset", "88");
  });

  it("keeps the drag strip for states that have no page header", async () => {
    render(<IssueWindow context={context} />);

    expect(await screen.findByTestId("workspace-loading")).toBeInTheDocument();
    expect(screen.getByTestId("drag-strip")).toBeInTheDocument();
    expect(screen.getByTestId("title-bar-scope")).toHaveAttribute("data-leading-inset", "");
  });
});
