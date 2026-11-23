import { afterEach, describe, expect, it } from "vitest";
import { cleanup, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { noteInboxAutoArchive, noteInboxBarAction } from "@multica/core/inbox/auto-archive";
import type { KeptInboxRow } from "@multica/core/inbox/open-row";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { InboxItem, Issue } from "@multica/core/types";
import { vi } from "vitest";
import { useAutoArchiveCause } from "./use-auto-archive-cause";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));

function item(id: string, issueId: string): InboxItem {
  return {
    id,
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: issueId,
    title: id,
    body: null,
    issue_status: "in_review",
    issue_priority: "none",
    read: true,
    archived: false,
    created_at: "2026-11-21T08:00:00Z",
    details: null,
  };
}

const archived = (issueId: string): KeptInboxRow => ({
  key: issueId,
  item: item(`n-${issueId}`, issueId),
  reason: "archived",
});

function setup(initial: KeptInboxRow | null = null) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const view = renderHook(
    ({ settled }: { settled: KeptInboxRow | null }) => useAutoArchiveCause(settled),
    { wrapper, initialProps: { settled: initial } },
  );
  return {
    settle: (row: KeptInboxRow | null) => view.rerender({ settled: row }),
    cause: () => view.result.current.cause,
    reason: () => view.result.current.reason,
    qc,
  };
}

afterEach(() => {
  cleanup();
});

describe("useAutoArchiveCause", () => {
  it("reports the reason for an agent-triggered archive", () => {
    const { settle, cause, reason } = setup();
    noteInboxAutoArchive({ issue_id: "issue-agent", reason: "actor_acted", trigger: "agent_triggered" });
    settle(archived("issue-agent"));
    expect(reason()).toBe("agent_triggered");
    expect(cause()).toBeNull();
  });

  it("reports the bar action and reason behind a user-caused archive", () => {
    const { settle, cause, reason } = setup();
    noteInboxBarAction("issue-f", { type: "status", status: "in_progress" });
    noteInboxAutoArchive({ issue_id: "issue-f", reason: "actor_acted" });
    settle(archived("issue-f"));
    expect(cause()).toEqual({ type: "status", status: "in_progress" });
    expect(reason()).toBe("actor_acted");
  });

  it("reports no assignment the issue does not show, as one dismissed at its confirmation", () => {
    const { settle, cause, reason, qc } = setup();
    qc.setQueryData(issueDetailOptions("ws", "issue-h").queryKey, {
      id: "issue-h",
      status: "in_review",
      assignee_type: "member",
      assignee_id: "member-1",
    } as Issue);
    noteInboxBarAction("issue-h", { type: "assignee", assignee: { type: "agent", id: "agent-nova" } });
    noteInboxAutoArchive({ issue_id: "issue-h", reason: "actor_acted" });
    settle(archived("issue-h"));
    expect(cause()).toBeNull();
    expect(reason()).toBe("actor_acted");
  });

  it("reports no action when something else archived it, and forgets the action", () => {
    const { settle, cause, reason } = setup();
    noteInboxBarAction("issue-g", { type: "status", status: "in_progress" });
    settle(archived("issue-g"));
    expect(cause()).toBeNull();
    expect(reason()).toBeNull();
    settle(null);
    noteInboxAutoArchive({ issue_id: "issue-g", reason: "actor_acted" });
    settle(archived("issue-g"));
    expect(cause()).toBeNull();
    expect(reason()).toBe("actor_acted");
  });
});
