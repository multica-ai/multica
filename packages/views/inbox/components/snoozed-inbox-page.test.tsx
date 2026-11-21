import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { snoozedInboxOptions } from "@multica/core/inbox/queries";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { Issue, SnoozedInboxIssue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { SnoozedInboxPage } from "./snoozed-inbox-page";

const state = vi.hoisted(() => ({
  snooze: vi.fn(() => Promise.resolve()),
  unsnooze: vi.fn(() => Promise.resolve()),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/inbox/mutations", () => ({
  useSnoozeInbox: () => ({ mutateAsync: state.snooze }),
  useUnsnoozeInbox: () => ({ mutateAsync: state.unsnooze }),
  cachedInboxSnoozedUntil: () => null,
}));

const HOUR = 60 * 60 * 1000;

function entry(issueId: string, until: Date, updates = 0): SnoozedInboxIssue {
  return {
    issue_id: issueId,
    item_id: `item-${issueId}`,
    snoozed_until: until.toISOString(),
    snoozed_at: new Date(Date.now() - HOUR).toISOString(),
    updates,
  };
}

function renderPage(snoozed: SnoozedInboxIssue[]) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(snoozedInboxOptions("ws-1").queryKey, snoozed);
  for (const { issue_id } of snoozed) {
    client.setQueryData(issueDetailOptions("ws-1", issue_id).queryKey, {
      id: issue_id,
      identifier: `MUL-${issue_id.slice(-1)}`,
      title: `Issue ${issue_id}`,
      status: "todo",
    } as Issue);
  }
  const nav: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues",
    searchParams: new URLSearchParams("view=snoozed"),
    hash: "",
    getShareableUrl: (path) => path,
  };
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <NavigationProvider value={nav}>
        <SnoozedInboxPage />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.snooze.mockClear();
  state.unsnooze.mockClear();
});

describe("SnoozedInboxPage", () => {
  it("lists snoozed issues by the day they come back, with what arrived meanwhile", () => {
    const now = Date.now();
    renderPage([entry("issue-1", new Date(now + HOUR), 3), entry("issue-2", new Date(now + 3 * 24 * HOUR))]);

    expect(screen.getByRole("heading", { level: 1, name: "Snoozed" })).toBeInTheDocument();
    expect(screen.getByText("Issue issue-1")).toBeInTheDocument();
    expect(screen.getByText("Issue issue-2")).toBeInTheDocument();
    expect(screen.getByText("3 updates while snoozed")).toBeInTheDocument();
  });

  it("brings an issue back now", () => {
    renderPage([entry("issue-1", new Date(Date.now() + HOUR))]);
    fireEvent.click(screen.getByRole("button", { name: /Bring back now/ }));
    expect(state.unsnooze).toHaveBeenCalledWith({ id: "item-issue-1" });
  });

  it("says when nothing is snoozed", () => {
    renderPage([]);
    expect(screen.getByText("Nothing snoozed")).toBeInTheDocument();
  });
});
