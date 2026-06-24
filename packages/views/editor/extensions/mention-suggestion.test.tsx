import { act, render, screen, waitFor } from "@testing-library/react";
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

// Mock the workspace id singleton — items() reads it imperatively.
vi.mock("@multica/core/platform", () => ({
  getCurrentWsId: () => "ws-1",
}));

// Mock the API so we control search responses + observe calls.
const searchIssuesMock = vi.fn();
const searchProjectsMock = vi.fn();
vi.mock("@multica/core/api", () => ({
  api: {
    get searchIssues() {
      return searchIssuesMock;
    },
    get searchProjects() {
      return searchProjectsMock;
    },
  },
}));

// Mock the auth store: items() reads `useAuthStore.getState()` imperatively
// to identify the current user when filtering personal agents.
const authState = { user: { id: "u1" } as { id: string } | null };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: { getState: () => authState },
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
  squads?: Array<{
    id: string;
    name: string;
    archived_at: string | null;
  }>;
  issues?: Array<{ id: string; identifier: string; title: string; status: string }>;
}): QueryClient {
  const map = new Map<string, unknown>();
  map.set(JSON.stringify(workspaceKeys.members("ws-1")), data.members ?? []);
  map.set(JSON.stringify(workspaceKeys.agents("ws-1")), data.agents ?? []);
  map.set(JSON.stringify(workspaceKeys.squads("ws-1")), data.squads ?? []);
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
    getQueriesData: <T,>(filter: { queryKey: readonly unknown[] }) => {
      const prefix = filter.queryKey as unknown[];
      const results: [readonly unknown[], T][] = [];
      for (const [k, v] of map) {
        const parsed = JSON.parse(k) as unknown[];
        if (parsed.length >= prefix.length && prefix.every((seg, i) => JSON.stringify(seg) === JSON.stringify(parsed[i]))) {
          results.push([parsed, v as T]);
        }
      }
      return results;
    },
  } as unknown as QueryClient;
}

describe("createMentionSuggestion", () => {
  beforeEach(() => {
    searchIssuesMock.mockReset();
    searchProjectsMock.mockReset();
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("returns members and agents synchronously without waiting for the server search", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
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

  it("loads server issue and project matches when project search is enabled", async () => {
    searchIssuesMock.mockResolvedValue({ issues: [], total: 0 });
    searchProjectsMock.mockResolvedValue({
      projects: [
        {
          id: "p-roadmap",
          title: "Roadmap",
          description: "Q3 planning",
          icon: null,
          status: "active",
        },
      ],
      total: 1,
    });

    render(
      <I18nWrapper>
        <MentionList items={[]} query="road" command={vi.fn()} includeProjectSearch />
      </I18nWrapper>,
    );

    await waitFor(() => {
      expect(screen.getByText("Roadmap")).toBeInTheDocument();
    });
    expect(searchIssuesMock).toHaveBeenCalledWith(expect.objectContaining({ q: "road", limit: 8 }));
    expect(searchProjectsMock).toHaveBeenCalledWith(expect.objectContaining({ q: "road", limit: 8 }));
  });

  it("does not call searchIssues for an empty query", () => {
    render(<I18nWrapper><MentionList items={[]} query="" command={vi.fn()} /></I18nWrapper>);

    expect(searchIssuesMock).not.toHaveBeenCalled();
    expect(searchProjectsMock).not.toHaveBeenCalled();
  });

  it("captures Enter while the popup has no selectable items", () => {
    const ref = createRef<MentionListRef>();

    render(<I18nWrapper><MentionList ref={ref} items={[]} query="协作" command={vi.fn()} /></I18nWrapper>);

    expect(
      ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", { key: "Enter" }) }),
    ).toBe(true);
  });

  // MUL-3607: groupItems() re-buckets the list (current → recent → search →
  // users → issues), so an item that sits LATER in the data array can render
  // NEAR THE TOP. Selection must follow the rendered order — otherwise the
  // highlighted row and the committed item drift apart and you mention the
  // neighbour of who you picked. (Issue rows are used because they render
  // without workspace/avatar context; the bug is type-agnostic.)
  it("commits the highlighted row, not its neighbour, when groups reorder the list", () => {
    const command = vi.fn<(item: MentionItem) => void>();
    const ref = createRef<MentionListRef>();

    // Data order is [MUL-2 (issues bucket), MUL-1 (search bucket)], but
    // groupItems hoists the search row, so the RENDERED order is [MUL-1, MUL-2].
    const items: MentionItem[] = [
      { id: "i-plain", label: "MUL-2", type: "issue" },
      { id: "i-search", label: "MUL-1", type: "issue", group: "search" },
    ];

    render(
      <I18nWrapper>
        <MentionList ref={ref} items={items} query="" command={command} includeProjectSearch />
      </I18nWrapper>,
    );

    const highlightedLabel = () => {
      const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>("button"));
      return buttons.find((b) => b.classList.contains("bg-accent"))?.textContent ?? "";
    };
    const press = (key: string) =>
      act(() => {
        ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", { key }) });
      });

    // First rendered row is the hoisted search result. Enter commits it, not
    // the issue that sits first in the data array.
    expect(highlightedLabel()).toBe("MUL-1");
    press("Enter");
    expect(command).toHaveBeenCalledTimes(1);
    expect(command.mock.calls[0]?.[0]?.label).toBe("MUL-1");

    command.mockClear();

    // Arrow down one row, then Enter — still commits exactly the highlighted row.
    press("ArrowDown");
    expect(highlightedLabel()).toBe("MUL-2");
    press("Enter");
    expect(command).toHaveBeenCalledTimes(1);
    expect(command.mock.calls[0]?.[0]?.label).toBe("MUL-2");
  });

  it("hides personal agents owned by someone else from a regular member", () => {
    const qc = fakeQc({
      members: [
        { user_id: "u1", name: "Alice", role: "member" },
        { user_id: "u2", name: "Bob", role: "member" },
      ],
      agents: [
        // Bob's personal agent — Alice (current user) should not see it.
        {
          id: "a-personal-bob",
          name: "Atlas",
          archived_at: null,
          visibility: "private",
          owner_id: "u2",
        },
        // Alice's own personal agent — should be visible.
        {
          id: "a-personal-alice",
          name: "Athena",
          archived_at: null,
          visibility: "private",
          owner_id: "u1",
        },
        // Workspace agent — visible to everyone.
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
    expect(items.some((i) => i.type === "agent" && i.label === "Aether")).toBe(true);
    expect(items.some((i) => i.type === "agent" && i.label === "Atlas")).toBe(false);
  });

  it("shows everyone's personal agents to a workspace admin", () => {
    // Role lives in the member fixture, not in authState — promoting Alice
    // to admin here is enough to flip the gate. Backend gate allows admins
    // to assign anyone's personal agent, so the @mention list mirrors that.
    const qc = fakeQc({
      members: [
        { user_id: "u1", name: "Alice", role: "admin" },
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

    expect(items.some((i) => i.type === "agent" && i.label === "Atlas")).toBe(true);
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

  it("does not inject current/recent chat context into the normal @ results", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      issues: [{ id: "i1", identifier: "MUL-1", title: "Login bug", status: "todo" }],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "", editor: {} as never }) as MentionItem[];

    expect(result.some((item) => item.group === "current" || item.group === "recent")).toBe(false);
    expect(result.map((item) => `${item.type}:${item.id}`)).toContain("member:u1");
    expect(result.map((item) => `${item.type}:${item.id}`)).toContain("issue:i1");
  });


  it("shows only current/recent chat context before the user types a query", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      agents: [{ id: "a1", name: "Aegis", archived_at: null, visibility: "workspace", owner_id: null }],
      issues: [{ id: "i-cache", identifier: "MUL-9", title: "Cached", status: "todo" }],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc, {
      mode: "context",
      getContextItems: () => [
        { id: "i1", label: "MUL-1", type: "issue", description: "Alpha issue", status: "todo", group: "current" },
        { id: "p1", label: "Roadmap", type: "project", description: "Q3", group: "recent" },
      ],
    });
    const result = config.items!({ query: "", editor: {} as never }) as MentionItem[];

    expect(result.map((item) => `${item.type}:${item.id}`)).toEqual(["issue:i1", "project:p1"]);
    expect(result.some((item) => item.type === "member" || item.type === "agent")).toBe(false);
  });

  it("prepends current/recent chat context without removing normal mention targets after the user types", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      agents: [{ id: "a1", name: "Aegis", archived_at: null, visibility: "workspace", owner_id: null }],
      issues: [{ id: "i-cache", identifier: "MUL-9", title: "Cached", status: "todo" }],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc, {
      mode: "context",
      getContextItems: () => [
        { id: "i1", label: "MUL-1", type: "issue", description: "Alpha issue", status: "todo", group: "current" },
        { id: "p1", label: "Roadmap", type: "project", description: "Q3", group: "recent" },
      ],
    });
    const result = config.items!({ query: "a", editor: {} as never }) as MentionItem[];

    expect(result.map((item) => `${item.type}:${item.id}`).slice(0, 2)).toEqual(["issue:i1", "project:p1"]);
    expect(result.some((item) => item.type === "member" && item.label === "Alice")).toBe(true);
    expect(result.some((item) => item.type === "agent" && item.label === "Aegis")).toBe(true);
  });

  it("renders current and recent sections for injected object mentions", () => {
    render(
      <I18nWrapper>
        <MentionList
          items={[
            { id: "i1", label: "MUL-1", type: "issue", description: "Login bug", group: "current" },
            { id: "p1", label: "Roadmap", type: "project", description: "Q3", group: "recent" },
          ]}
          query=""
          command={vi.fn()}
        />
      </I18nWrapper>,
    );

    expect(screen.getByText("Current page")).toBeInTheDocument();
    expect(screen.getByText("Recently viewed")).toBeInTheDocument();
    expect(screen.getByText("MUL-1")).toBeInTheDocument();
    expect(screen.getByText("Roadmap")).toBeInTheDocument();
  });

  it("includes all non-archived squads in the mention list", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      squads: [
        { id: "s1", name: "Jiayuan's Coding Team", archived_at: null },
        { id: "s2", name: "独立团", archived_at: null },
        { id: "s3", name: "Archived Squad", archived_at: "2026-01-01T00:00:00Z" },
      ],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "", editor: {} as never });

    const items = result as MentionItem[];
    expect(items.filter((i) => i.type === "squad")).toHaveLength(2);
    expect(items.some((i) => i.type === "squad" && i.label === "Jiayuan's Coding Team")).toBe(true);
    expect(items.some((i) => i.type === "squad" && i.label === "独立团")).toBe(true);
    expect(items.some((i) => i.type === "squad" && i.label === "Archived Squad")).toBe(false);
  });

  it("returns no squads when the squads cache is empty (not yet fetched)", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      // squads not provided — simulates cache miss
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "", editor: {} as never });

    const items = result as MentionItem[];
    expect(items.filter((i) => i.type === "squad")).toHaveLength(0);
  });

  it("matches Chinese names by full pinyin", () => {
    const qc = fakeQc({
      members: [
        { user_id: "u1", name: "Alice", role: "member" },
        { user_id: "u2", name: "李云龙", role: "member" },
      ],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "liyunlong", editor: {} as never });

    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "member" && i.label === "李云龙")).toBe(true);
    expect(items.some((i) => i.type === "member" && i.label === "Alice")).toBe(false);
  });

  it("matches Chinese names by pinyin initials", () => {
    const qc = fakeQc({
      members: [
        { user_id: "u1", name: "Alice", role: "member" },
        { user_id: "u2", name: "李云龙", role: "member" },
        { user_id: "u3", name: "张大彪", role: "member" },
      ],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "lyl", editor: {} as never });

    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "member" && i.label === "李云龙")).toBe(true);
    expect(items.some((i) => i.type === "member" && i.label === "张大彪")).toBe(false);
  });

  it("matches Chinese agent names by pinyin", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      agents: [
        { id: "a1", name: "魏和尚", archived_at: null, visibility: "workspace", owner_id: null },
      ],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!({ query: "whs", editor: {} as never });

    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "agent" && i.label === "魏和尚")).toBe(true);
  });
});
