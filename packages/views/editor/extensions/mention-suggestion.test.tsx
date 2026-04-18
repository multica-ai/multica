import { describe, it, expect, vi, beforeEach } from "vitest";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { issueKeys } from "@multica/core/issues/queries";
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

import { createMentionSuggestion, type MentionItem } from "./mention-suggestion";

function fakeQc(data: {
  members?: Array<{ user_id: string; name: string }>;
  agents?: Array<{ id: string; name: string; archived_at: string | null }>;
  issues?: Array<{ id: string; identifier: string; title: string; status: string }>;
}): QueryClient {
  const map = new Map<string, unknown>();
  map.set(JSON.stringify(workspaceKeys.members("ws-1")), data.members ?? []);
  map.set(JSON.stringify(workspaceKeys.agents("ws-1")), data.agents ?? []);
  map.set(JSON.stringify(issueKeys.list("ws-1")), {
    issues: data.issues ?? [],
    total: data.issues?.length ?? 0,
  });
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

  it("calls searchIssues with include_closed=true so done issues are findable", async () => {
    const qc = fakeQc({});
    searchIssuesMock.mockResolvedValue({ issues: [], total: 0 });

    const config = createMentionSuggestion(qc);
    config.items!({ query: "bug-xyz", editor: {} as never });

    // Wait past the 150ms debounce.
    await new Promise((r) => setTimeout(r, 200));

    expect(searchIssuesMock).toHaveBeenCalledWith(
      expect.objectContaining({ q: "bug-xyz", include_closed: true }),
    );
  });

  it("does not call searchIssues for an empty query", async () => {
    const qc = fakeQc({});
    searchIssuesMock.mockResolvedValue({ issues: [], total: 0 });

    const config = createMentionSuggestion(qc);
    config.items!({ query: "", editor: {} as never });

    await new Promise((r) => setTimeout(r, 200));
    // No call with an empty q (other tests' fire-and-forget closures may leak,
    // so assert on the *content* of any call rather than absence).
    for (const call of searchIssuesMock.mock.calls) {
      expect(call[0].q).not.toBe("");
    }
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
