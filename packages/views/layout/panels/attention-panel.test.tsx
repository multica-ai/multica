import { useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { inboxKeys } from "@multica/core/inbox/queries";
import type { InboxItem } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { ATTENTION_PREVIEW_LIMIT, AttentionPanel } from "./attention-panel";

const writeViewState = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("acme"),
    useCurrentWorkspace: () => ({ id: "ws-1" }),
  };
});
vi.mock("@multica/core/auth", async (importOriginal) => {
  const state = { user: { id: "u1" } };
  return {
    ...(await importOriginal<typeof import("@multica/core/auth")>()),
    useAuthStore: (selector: (s: typeof state) => unknown) => selector(state),
  };
});
vi.mock("@multica/core/inbox/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/inbox/queries")>()),
  useInboxUnreadCount: () => 3,
}));
vi.mock("@multica/core/issue-statuses/hooks", async () => {
  const { buildIssueStatusCatalog } = await import("@multica/core/issue-statuses/queries");
  return { useIssueStatuses: () => buildIssueStatusCatalog([]) };
});
vi.mock("./pinned-section", () => ({
  usePanelPins: () => null,
  PinnedSection: () => <div data-testid="pinned" />,
}));
vi.mock("../../issues/actions", () => ({
  IssueContextMenuProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("../../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
vi.mock("../../issues/components/issue-detail", () => ({
  issueHighlightMementoKey: (issueId: string) => `highlight:${issueId}`,
}));
vi.mock("../../platform", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../../platform")>()),
  useViewStateWriter: () => writeViewState,
}));

function item(n: number): InboxItem {
  return {
    id: `inbox-${n}`,
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "u1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: `issue-${n}`,
    title: `Issue ${n}`,
    body: null,
    issue_status: "todo",
    issue_priority: null,
    read: true,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
  };
}

function renderPanel(initialUrl: string, items: InboxItem[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(inboxKeys.list("ws-1"), items);

  function Harness() {
    const [url, setUrl] = useState(initialUrl);
    const [pathname = "", query = ""] = url.split("?");
    const adapter: NavigationAdapter = {
      push: setUrl,
      replace: setUrl,
      back: vi.fn(),
      pathname,
      searchParams: new URLSearchParams(query),
      hash: "",
      getShareableUrl: (path) => path,
    };
    return (
      <NavigationProvider value={adapter}>
        <AttentionPanel />
      </NavigationProvider>
    );
  }

  renderWithI18n(
    <QueryClientProvider client={qc}>
      <Harness />
    </QueryClientProvider>,
  );
  return qc;
}

const row = (n: number) => document.querySelector<HTMLElement>(`a[href="/acme/inbox?issue=issue-${n}"]`);
const needsAction = () => screen.getByRole("link", { name: /^Needs action/ });

describe("AttentionPanel", () => {
  it("lists the first waiting issues under Needs action and links to the rest", () => {
    const items = Array.from({ length: ATTENTION_PREVIEW_LIMIT + 2 }, (_, i) => item(i + 1));
    renderPanel("/acme/issues", items);

    expect(screen.getByTestId("pinned")).toBeInTheDocument();
    expect(needsAction()).toHaveAttribute("href", "/acme/inbox");
    expect(needsAction()).toHaveTextContent("3");
    expect(row(1)).toHaveTextContent("Issue 1");
    expect(row(ATTENTION_PREVIEW_LIMIT)).not.toBeNull();
    expect(row(ATTENTION_PREVIEW_LIMIT + 1)).toBeNull();
    expect(screen.getByRole("link", { name: `View all ${ATTENTION_PREVIEW_LIMIT + 2}` })).toHaveAttribute(
      "href",
      "/acme/inbox",
    );
  });

  it("lights the page's open issue, or the group while the page lists them all", () => {
    renderPanel("/acme/inbox", [item(1), item(2)]);
    expect(needsAction()).toHaveAttribute("aria-current", "page");

    fireEvent.click(row(2)!);

    expect(row(2)).toHaveAttribute("aria-current", "page");
    expect(needsAction()).not.toHaveAttribute("aria-current");
    expect(writeViewState).toHaveBeenCalledWith("highlight:issue-2", undefined);
  });

  it("keeps a handled issue in place, dimmed, until another one opens", async () => {
    const qc = renderPanel("/acme/inbox?issue=issue-2", [item(1), item(2), item(3)]);

    act(() => qc.setQueryData(inboxKeys.list("ws-1"), [item(1), item(3)]));
    await waitFor(() => expect(row(2)).toHaveAttribute("data-handled", "true"));
    expect([row(1), row(3)].every(Boolean)).toBe(true);

    fireEvent.click(row(1)!);

    expect(row(2)).toBeNull();
  });

  it("says so when nothing is waiting", () => {
    renderPanel("/acme/inbox", []);
    expect(screen.getByText("Nothing needs you right now")).toBeInTheDocument();
  });
});
