import { useIssueOpeningStore } from "@multica/core/issues/stores/issue-opening-store";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Issue } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import {
  IssuePeekActionsContext,
  IssuePeekIdContext,
  type IssuePeekActions,
} from "../surface/peek-context";
import { ListRow } from "./list-row";

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: () => ({ data: [] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/properties", () => ({
  propertyListOptions: () => ({ queryKey: ["properties"] }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector: (state: unknown) => unknown) =>
    selector({ cardProperties: {}, cardPropertyIds: [] }),
}));

vi.mock("../../i18n", () => ({
  useLocale: () => "en",
  useT: () => ({ t: () => "Translated" }),
}));

vi.mock("../actions", () => ({
  IssueActionsContextMenu: ({ children }: { children: ReactNode }) => children,
}));

vi.mock("./issue-agent-activity-indicator", () => ({
  IssueAgentActivityIndicator: () => null,
}));

vi.mock("./custom-status-chip", () => ({
  CustomStatusChip: () => null,
}));

const navigation: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => `https://app.example${path}`,
};

const peek: IssuePeekActions = {
  open: vi.fn(),
  toggle: vi.fn(),
  close: vi.fn(),
  publishColumns: vi.fn(),
};

const issue = {
  id: "issue-1",
  identifier: "MUL-1",
  title: "Peekable row",
  status: "todo",
  priority: "none",
  labels: [],
  properties: {},
} as unknown as Issue;

function renderRow(peekedId: string | null = null) {
  return render(
    <NavigationProvider value={navigation}>
      <IssuePeekActionsContext.Provider value={peek}>
        <IssuePeekIdContext.Provider value={peekedId}>
          <ListRow issue={issue} />
        </IssuePeekIdContext.Provider>
      </IssuePeekActionsContext.Provider>
    </NavigationProvider>,
  );
}

describe("ListRow side peek", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useIssueOpeningStore.setState({ openMode: "page" });
  });

  it("opens the peek on Shift+Click and still navigates on a plain click", () => {
    renderRow();
    expect(fireEvent.click(screen.getByRole("link"), { shiftKey: true })).toBe(false);
    expect(peek.toggle).toHaveBeenCalledWith("issue-1");
    expect(navigation.push).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("link"));
    expect(navigation.push).toHaveBeenCalledWith("/acme/issues/issue-1");
  });

  it("opens on a plain click when side preview is preferred", () => {
    useIssueOpeningStore.getState().setOpenMode("peek");
    renderRow();
    fireEvent.click(screen.getByRole("link"));
    expect(peek.open).toHaveBeenCalledWith("issue-1");
    expect(navigation.push).not.toHaveBeenCalled();
  });

  it("marks the peeked row as the peek target", () => {
    const { container } = renderRow("issue-1");
    const row = container.querySelector('[data-peek-target="issue-1"]');
    expect(row).toHaveAttribute("data-peeked");
  });
});
