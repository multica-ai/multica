import { render, screen, waitFor } from "@testing-library/react";
import { createRef } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { issueKeys, PAGINATED_STATUSES } from "@multica/core/issues/queries";
import type { IssueStatus, ListIssuesCache } from "@multica/core/types";
import type { QueryClient } from "@tanstack/react-query";

// Mock the workspace id singleton — items() reads it imperatively.
vi.mock("@multica/core/platform", () => ({
  getCurrentWsId: () => "ws-1",
}));

// Mock the API so we control searchIssues responses + observe calls.
const searchIssuesMock = vi.fn();
vi.mock("@multica/core/api", () => ({
  api: {
    get searchIssues() {
      return searchIssuesMock;
    },
  },
}));

vi.mock("@multica/i18n/react", async () => {
  const { en } = await import("@multica/i18n/dict/en");
  return {
    useT: (ns?: string) => (key: string, params?: Record<string, string | number>) => {
      const template = ns ? en[ns]?.[key] ?? key : key;
      if (!params) return template;
      return template.replace(/\{(\w+)\}/g, (_, k: string) => String(params[k] ?? `{${k}}`));
    },
  };
});

import {
  createMentionSuggestion,
  MentionList,
  type MentionListRef,
  type MentionItem,
} from "./mention-suggestion";

function fakeQc(data: {
  members?: Array<{ user_id: string; name: string }>;
  agents?: Array<{ id: string; name: string; archived_at: string | null }>;
  issues?: Array<{ id: string; identifier: string; title: string; status: string }>;
}): QueryClient {
  const map = new Map<string, unknown>();
  map.set(JSON.stringify(workspaceKeys.members("ws-1")), data.members ?? []);
  map.set(JSON.stringify(workspaceKeys.agents("ws-1")), data.agents ?? []);
  const byStatus: ListIssuesCache["byStatus"] = {};
  for (const status of PAGINATED_STATUSES) {
    const bucket = (data.issues ?? []).filter((i) => i.status === status);
    byStatus[status as IssueStatus] = { issues: bucket as never, total: bucket.length };
  }
  map.set(
    JSON.stringify(issueKeys.list("ws-1")),
    { byStatus } satisfies ListIssuesCache,
  );
  return {
    getQueryData: (key: readonly unknown[]) => map.get(JSON.stringify(key)),
  } as unknown as QueryClient;
}

describe("createMentionSuggestion", () => {
  beforeEach(() => {
    searchIssuesMock.mockReset();
  });

  it("returns members and agents synchronously without waiting for the server search", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice" }],
      agents: [{ id: "a1", name: "Aegis", archived_at: null }],
    });
    // A pending fetch — would block the result if items() awaited it.
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "a", editor: {} as never });

    // Must be synchronous: a plain array, not a Promise.
    expect(Array.isArray(result)).toBe(true);
    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "member" && i.label === "Alice")).toBe(true);
    expect(items.some((i) => i.type === "agent" && i.label === "Aegis")).toBe(true);
  });

  it("loads server issue matches into the popup when the list cache misses", async () => {
    searchIssuesMock.mockResolvedValue({
      issues: [
        {
          id: "i-1007",
          identifier: "MUL-1007",
          title: "多 Agent 协作探索",
          status: "done",
        },
      ],
      total: 1,
    });

    render(<MentionList items={[]} query="协作" command={vi.fn()} />);

    expect(screen.getByText("Searching...")).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByText("MUL-1007")).toBeInTheDocument();
    });
    expect(screen.getByText("多 Agent 协作探索")).toBeInTheDocument();
    expect(searchIssuesMock).toHaveBeenCalledWith(
      expect.objectContaining({
        q: "协作",
        limit: 20,
        include_closed: true,
      }),
    );
  });

  it("does not call searchIssues for an empty query", () => {
    render(<MentionList items={[]} query="" command={vi.fn()} />);

    expect(searchIssuesMock).not.toHaveBeenCalled();
  });

  it("captures Enter while the popup has no selectable items", () => {
    const ref = createRef<MentionListRef>();

    render(<MentionList ref={ref} items={[]} query="协作" command={vi.fn()} />);

    expect(
      ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", { key: "Enter" }) }),
    ).toBe(true);
  });

  it("includes cached issues in the synchronous response", () => {
    const qc = fakeQc({
      issues: [
        { id: "i1", identifier: "MUL-1", title: "Login bug", status: "todo" },
        { id: "i2", identifier: "MUL-2", title: "Other", status: "done" },
      ],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "bug", editor: {} as never });

    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "issue" && i.id === "i1")).toBe(true);
  });
});
