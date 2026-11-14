import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { InboxWorkingRow } from "@multica/core/inbox/sections";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import type { Issue } from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { renderWithI18n } from "../../test/i18n";
import { InboxWorkingItem } from "./inbox-working-item";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => `Agent ${id}` }),
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: { actorType: string; actorId: string }) => (
    <span data-testid="avatar" data-actor={`${actorType}:${actorId}`} />
  ),
}));

const ROW: InboxWorkingRow = {
  issueId: "issue-1",
  taskId: "task-1",
  agentId: "agent-1",
  queued: false,
  since: "2026-06-17T14:16:00Z",
};

function renderRow({
  row = ROW,
  issue,
  getIssue,
  adapter = {},
}: {
  row?: InboxWorkingRow;
  issue?: Partial<Issue>;
  getIssue?: () => Promise<Issue>;
  adapter?: Partial<NavigationAdapter>;
} = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  if (issue) qc.setQueryData(issueDetailOptions("ws-1", row.issueId).queryKey, issue as Issue);
  setApiInstance({ getIssue: getIssue ?? (() => new Promise<Issue>(() => {})) } as unknown as ApiClient);
  const onClick = vi.fn();
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/inbox",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
    ...adapter,
  };
  const utils = renderWithI18n(
    <QueryClientProvider client={qc}>
      <WorkspaceSlugProvider slug="acme">
        <NavigationProvider value={navigation}>
          <InboxWorkingItem row={row} isSelected={false} onClick={onClick} />
        </NavigationProvider>
      </WorkspaceSlugProvider>
    </QueryClientProvider>,
  );
  return { ...utils, onClick };
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-06-17T14:20:00Z"));
});
afterEach(() => {
  vi.useRealTimers();
});

describe("InboxWorkingItem", () => {
  it("names the issue the agent is on, and how long it has been running", () => {
    renderRow({ issue: { id: "issue-1", title: "Search the work panel" } });

    const row = screen.getByRole("button");
    expect(row).toHaveTextContent("Search the work panel");
    expect(row).toHaveTextContent("4m");
    expect(screen.getByTestId("avatar")).toHaveAttribute("data-actor", "agent:agent-1");
    expect(row.querySelector(".bg-success")).not.toBeNull();
  });

  it("says a run that has not started is queued, without the live dot", () => {
    renderRow({ row: { ...ROW, queued: true }, issue: { id: "issue-1", title: "Search the work panel" } });

    const row = screen.getByRole("button");
    expect(row).toHaveTextContent("Queued");
    expect(row.querySelector(".bg-success")).toBeNull();
  });

  it("falls back to the agent's name when the issue cannot be read", async () => {
    renderRow({ getIssue: () => Promise.reject(new Error("forbidden")) });

    expect(await screen.findByText("Agent agent-1")).toBeInTheDocument();
  });

  it("opens the issue in the detail pane on a plain click", () => {
    const { onClick } = renderRow({ issue: { id: "issue-1", title: "Search the work panel" } });

    fireEvent.click(screen.getByRole("button"));

    expect(onClick).toHaveBeenCalledTimes(1);
  });

  it("opens the issue as its own tab on a modifier click", () => {
    const openInNewTab = vi.fn();
    const { onClick } = renderRow({
      issue: { id: "issue-1", title: "Search the work panel" },
      adapter: { openInNewTab },
    });

    fireEvent.click(screen.getByRole("button"), { metaKey: true });

    expect(onClick).not.toHaveBeenCalled();
    expect(openInNewTab).toHaveBeenCalledWith("/acme/issues/issue-1", undefined);
  });
});
