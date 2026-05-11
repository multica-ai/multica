import { render, screen, waitFor } from "@testing-library/react";
import { createRef, type ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { issueKeys, PAGINATED_STATUSES } from "@multica/core/issues/queries";
import { I18nProvider } from "@multica/core/i18n/react";
import type { IssueStatus, ListIssuesCache } from "@multica/core/types";
import type { QueryClient } from "@tanstack/react-query";
import enCommon from "../../locales/en/common.json";
import enAuth from "../../locales/en/auth.json";
import enSettings from "../../locales/en/settings.json";
import enEditor from "../../locales/en/editor.json";

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth, settings: enSettings, editor: enEditor },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const authState = vi.hoisted(() => ({ userId: "u-current" }));

// Mock the workspace id singleton — items() reads it imperatively.
vi.mock("@multica/core/platform", () => ({
  getCurrentWsId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: {
    getState: () => ({ user: { id: authState.userId } }),
  },
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

import {
  createMentionSuggestion,
  MentionList,
  type MentionListRef,
  type MentionItem,
} from "./mention-suggestion";

function fakeQc(data: {
  members?: Array<{ user_id: string; name: string; role?: string }>;
  agents?: Array<{
    id: string;
    name: string;
    archived_at: string | null;
    visibility?: "workspace" | "private";
    owner_id?: string | null;
  }>;
  issues?: Array<{ id: string; identifier: string; title: string; status: string }>;
  mentionFrequency?: Array<{ actor_type: string; actor_id: string; frequency: number; last_mentioned_at: string }>;
}): QueryClient {
  const map = new Map<string, unknown>();
  map.set(JSON.stringify(workspaceKeys.members("ws-1")), data.members ?? []);
  map.set(JSON.stringify(workspaceKeys.agents("ws-1")), data.agents ?? []);
  map.set(JSON.stringify(workspaceKeys.mentionFrequency("ws-1")), data.mentionFrequency ?? []);
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
    authState.userId = "u-current";
  });

  it("returns members and agents synchronously without waiting for the server search", () => {
    const qc = fakeQc({
      members: [
        { user_id: "u-current", name: "CurrentUser", role: "member" },
        { user_id: "u1", name: "Alice", role: "member" },
      ],
      agents: [
        {
          id: "a1",
          name: "Aegis",
          archived_at: null,
          visibility: "workspace",
          owner_id: null,
        },
      ],
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

    render(<I18nWrapper><MentionList items={[]} query="协作" command={vi.fn()} /></I18nWrapper>);

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

  it("filters agents owned by other users from mention suggestions", () => {
    const qc = fakeQc({
      members: [{ user_id: "u-current", name: "CurrentUser", role: "member" }],
      agents: [
        {
          id: "own-private",
          name: "Own Private",
          archived_at: null,
          visibility: "private",
          owner_id: "u-current",
        },
        {
          id: "other-workspace",
          name: "Other Workspace",
          archived_at: null,
          visibility: "workspace",
          owner_id: "u-other",
        },
        {
          id: "other-private",
          name: "Other Private",
          archived_at: null,
          visibility: "private",
          owner_id: "u-other",
        },
      ],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "", editor: {} as never }) as MentionItem[];
    const agentIds = result.filter((i) => i.type === "agent").map((i) => i.id);

    expect(agentIds).toContain("own-private");
    // Workspace-visible agents owned by other users are now hidden too.
    expect(agentIds).not.toContain("other-workspace");
    expect(agentIds).not.toContain("other-private");
  });

  it("calls searchIssues with include_closed=true so done issues are findable", async () => {
    searchIssuesMock.mockResolvedValue({ issues: [], total: 0 });

    render(
      <I18nWrapper>
        <MentionList items={[]} query="bug-xyz" command={vi.fn()} />
      </I18nWrapper>,
    );

    // Wait past the 150ms debounce.
    await new Promise((r) => setTimeout(r, 200));

    expect(searchIssuesMock).toHaveBeenCalledWith(
      expect.objectContaining({ q: "bug-xyz", include_closed: true }),
    );
  });

  it("does not call searchIssues for an empty query", () => {
    render(<I18nWrapper><MentionList items={[]} query="" command={vi.fn()} /></I18nWrapper>);

    expect(searchIssuesMock).not.toHaveBeenCalled();
  });

  it("captures Enter while the popup has no selectable items", () => {
    const ref = createRef<MentionListRef>();

    render(<I18nWrapper><MentionList ref={ref} items={[]} query="协作" command={vi.fn()} /></I18nWrapper>);

    expect(
      ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", { key: "Enter" }) }),
    ).toBe(true);
  });

  it("hides all agents owned by someone else from a regular member", () => {
    const qc = fakeQc({
      members: [
        { user_id: "u-current", name: "CurrentUser", role: "member" },
        { user_id: "u1", name: "Alice", role: "member" },
        { user_id: "u2", name: "Bob", role: "member" },
      ],
      agents: [
        // Bob's personal agent — current user should not see it.
        {
          id: "a-personal-bob",
          name: "Atlas",
          archived_at: null,
          visibility: "private",
          owner_id: "u2",
        },
        // Current user's own personal agent — should be visible.
        {
          id: "a-personal-alice",
          name: "Athena",
          archived_at: null,
          visibility: "private",
          owner_id: "u-current",
        },
        // Workspace agent owned by Bob — hidden from regular members.
        {
          id: "a-shared",
          name: "Aether",
          archived_at: null,
          visibility: "workspace",
          owner_id: "u2",
        },
      ],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "a", editor: {} as never });
    const items = result as MentionItem[];

    expect(items.some((i) => i.type === "agent" && i.label === "Athena")).toBe(true);
    expect(items.some((i) => i.type === "agent" && i.label === "Aether")).toBe(false);
    expect(items.some((i) => i.type === "agent" && i.label === "Atlas")).toBe(false);
  });

  it("does not show other users' personal agents to a workspace admin", () => {
    // Even admins should not see or @mention agents owned by other users —
    // agent visibility in the @mention list is strictly per owner_id.
    const qc = fakeQc({
      members: [
        { user_id: "u-current", name: "CurrentUser", role: "admin" },
        { user_id: "u2", name: "Bob", role: "member" },
      ],
      agents: [
        {
          id: "a-personal-bob",
          name: "Atlas",
          archived_at: null,
          visibility: "private",
          owner_id: "u2",
        },
      ],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "a", editor: {} as never });
    const items = result as MentionItem[];

    expect(items.some((i) => i.type === "agent" && i.label === "Atlas")).toBe(false);
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

  it("sorts member/agent items by recent mention frequency ranking", () => {
    const qc = fakeQc({
      members: [
        { user_id: "u-current", name: "CurrentUser", role: "member" },
        { user_id: "u1", name: "Alice" },
        { user_id: "u2", name: "Bob" },
      ],
      agents: [{ id: "a1", name: "Aegis", archived_at: null, visibility: "workspace", owner_id: null }],
      mentionFrequency: [
        {
          actor_type: "member",
          actor_id: "u2",
          frequency: 5,
          last_mentioned_at: "2026-04-29T06:00:00Z",
        },
        {
          actor_type: "agent",
          actor_id: "a1",
          frequency: 2,
          last_mentioned_at: "2026-04-29T05:00:00Z",
        },
      ],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    // Use a query that won't match "all members" so allItem is excluded.
    const result = config.items!({ query: "bo", editor: {} as never }) as MentionItem[];
    const users = result.filter((i) => i.type !== "issue" && i.type !== "all");

    // Bob (u2) has highest frequency, should be first.
    expect(users[0]?.type).toBe("member");
    expect(users[0]?.id).toBe("u2");
  });
});
