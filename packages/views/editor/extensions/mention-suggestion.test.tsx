import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createRef, type ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { issueKeys, PAGINATED_CATEGORIES } from "@multica/core/issues/queries";
import { I18nProvider } from "@multica/core/i18n/react";
import type { IssueStatusCategory, ListIssuesCache } from "@multica/core/types";
import type { QueryClient } from "@tanstack/react-query";
import enCommon from "../../locales/en/common.json";
import enAuth from "../../locales/en/auth.json";
import enSettings from "../../locales/en/settings.json";
import enEditor from "../../locales/en/editor.json";
import enIssues from "../../locales/en/issues.json";
import zhCommon from "../../locales/zh-Hans/common.json";
import zhAuth from "../../locales/zh-Hans/auth.json";
import zhSettings from "../../locales/zh-Hans/settings.json";
import zhEditor from "../../locales/zh-Hans/editor.json";
import zhIssues from "../../locales/zh-Hans/issues.json";

const TEST_RESOURCES = {
  en: {
    common: enCommon,
    auth: enAuth,
    settings: enSettings,
    editor: enEditor,
    issues: enIssues,
  },
  "zh-Hans": {
    common: zhCommon,
    auth: zhAuth,
    settings: zhSettings,
    editor: zhEditor,
    issues: zhIssues,
  },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

function ZhI18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="zh-Hans" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

// Mock the workspace id singleton — items() reads it imperatively.
vi.mock("@multica/core/platform", () => ({
  getCurrentWsId: () => "ws-1",
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => ({
    colorOf: (status: string) =>
      status === "awaiting_response" ? "#f97316" : null,
  }),
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

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`actor-${actorId}`} />
  ),
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
    runtime_id?: string;
    runtime_bound?: boolean;
    visibility?: "workspace" | "private";
    owner_id?: string | null;
    permission_mode?: "private" | "public_to";
    invocation_targets?: Array<{
      target_type: "workspace" | "member" | "team";
      target_id: string | null;
    }>;
  }>;
  squads?: Array<{
    id: string;
    name: string;
    archived_at: string | null;
    leader_id: string;
  }>;
  issues?: Array<{ id: string; identifier: string; title: string; status: string }>;
}): QueryClient {
  const map = new Map<string, unknown>();
  map.set(JSON.stringify(workspaceKeys.members("ws-1")), data.members ?? []);
  // MUL-3963: the mention filter runs through canAssignAgentToIssue, which
  // reads permission_mode + invocation_targets (not the legacy `visibility`).
  // Fixtures still express intent via `visibility`, so derive the permission
  // fields from it here (public_to + workspace target for "workspace";
  // private + no targets otherwise) unless a fixture sets them explicitly.
  const agentsWithPermissions = (data.agents ?? []).map((a) => ({
    ...a,
    runtime_id: a.runtime_id ?? "runtime-1",
    runtime_bound: a.runtime_bound ?? true,
    permission_mode:
      a.permission_mode ?? (a.visibility === "private" ? "private" : "public_to"),
    invocation_targets:
      a.invocation_targets ??
      (a.visibility === "private"
        ? []
        : [{ target_type: "workspace" as const, target_id: null }]),
  }));
  map.set(JSON.stringify(workspaceKeys.agents("ws-1")), agentsWithPermissions);
  map.set(JSON.stringify(workspaceKeys.squads("ws-1")), data.squads ?? []);
  const byStatus: ListIssuesCache["byStatus"] = {};
  for (const status of PAGINATED_CATEGORIES) {
    const bucket = (data.issues ?? []).filter((i) => i.status === status);
    byStatus[status as IssueStatusCategory] = { issues: bucket as never, total: bucket.length };
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

function itemArgs(query: string) {
  return {
    query,
    editor: {} as never,
    signal: new AbortController().signal,
  };
}

describe("createMentionSuggestion", () => {
  beforeEach(() => {
    searchIssuesMock.mockReset();
    searchProjectsMock.mockReset();
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("keeps the mention query active across spaces for multi-word search", () => {
    const qc = fakeQc({
      issues: [
        {
          id: "i-login",
          identifier: "MUL-1",
          title: "Login redirect bug",
          status: "todo",
        },
      ],
    });
    const config = createMentionSuggestion(qc);

    expect(config.allowSpaces).toBe(true);
    expect(config.items!(itemArgs("login redirect"))).toEqual([
      expect.objectContaining({ id: "i-login", type: "issue" }),
    ]);
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
    const result = config.items!(itemArgs("a"));

    // Must be synchronous: a plain array, not a Promise.
    expect(Array.isArray(result)).toBe(true);
    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "member" && i.label === "Alice")).toBe(true);
    expect(items.some((i) => i.type === "agent" && i.label === "Aegis")).toBe(true);
  });

  it("keeps an unbound agent discoverable but marks it as unselectable", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      agents: [
        {
          id: "a1",
          name: "Aegis",
          archived_at: null,
          runtime_id: "",
          runtime_bound: false,
          visibility: "workspace",
          owner_id: null,
        },
      ],
    });

    const config = createMentionSuggestion(qc);
    const items = config.items!(itemArgs("a")) as MentionItem[];

    expect(items).toContainEqual(
      expect.objectContaining({
        type: "agent",
        id: "a1",
        disabledReason: "agent_runtime_required",
      }),
    );
  });

  it("does not select a runtime-required mention row by click or keyboard", () => {
    const command = vi.fn<(item: MentionItem) => void>();
    const ref = createRef<MentionListRef>();
    render(
      <I18nWrapper>
        <MentionList
          ref={ref}
          items={[
            {
              id: "a1",
              label: "Aegis",
              type: "agent",
              disabledReason: "agent_runtime_required",
            },
          ]}
          query=""
          command={command}
        />
      </I18nWrapper>,
    );

    const row = screen.getByRole("button", {
      name: "Aegis: This target has no runtime — bind one to run it",
    });
    expect(row).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(row);
    expect(
      ref.current?.onKeyDown({
        event: new KeyboardEvent("keydown", { key: "Enter" }),
      }),
    ).toBe(true);
    expect(command).not.toHaveBeenCalled();
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

  // MUL-3685: plain Tab accepts the highlighted row exactly like Enter.
  it("accepts the highlighted row on plain Tab, like Enter", () => {
    const command = vi.fn<(item: MentionItem) => void>();
    const ref = createRef<MentionListRef>();
    const items: MentionItem[] = [
      { id: "i-1", label: "MUL-1", type: "issue" },
      { id: "i-2", label: "MUL-2", type: "issue" },
    ];

    render(
      <I18nWrapper>
        <MentionList ref={ref} items={items} query="" command={command} />
      </I18nWrapper>,
    );

    const handled = ref.current?.onKeyDown({
      event: new KeyboardEvent("keydown", { key: "Tab" }),
    });

    expect(handled).toBe(true);
    expect(command).toHaveBeenCalledTimes(1);
    expect(command.mock.calls[0]?.[0]?.label).toBe("MUL-1");
  });

  // Shift+Tab and any modifier+Tab stay focus navigation — they must NOT
  // accept, so the picker never traps reverse Tab traversal or OS switching.
  it("does not accept on Shift+Tab or modifier+Tab", () => {
    const command = vi.fn<(item: MentionItem) => void>();
    const ref = createRef<MentionListRef>();
    const items: MentionItem[] = [{ id: "i-1", label: "MUL-1", type: "issue" }];

    render(
      <I18nWrapper>
        <MentionList ref={ref} items={items} query="" command={command} />
      </I18nWrapper>,
    );

    const press = (init: KeyboardEventInit) =>
      ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", init) });

    expect(press({ key: "Tab", shiftKey: true })).toBe(false);
    expect(press({ key: "Tab", metaKey: true })).toBe(false);
    expect(press({ key: "Tab", ctrlKey: true })).toBe(false);
    expect(press({ key: "Tab", altKey: true })).toBe(false);
    expect(command).not.toHaveBeenCalled();
  });

  it("captures Tab while the popup has no selectable items, like Enter", () => {
    const ref = createRef<MentionListRef>();

    render(<I18nWrapper><MentionList ref={ref} items={[]} query="协作" command={vi.fn()} /></I18nWrapper>);

    expect(
      ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", { key: "Tab" }) }),
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

  // MUL-5495: the command bar (cmdk) navigates on Ctrl+N/J and Ctrl+P/K as well
  // as the arrows. The mention picker used to accept arrows only, so the same
  // muscle memory silently did nothing here.
  it("navigates with Ctrl+N/J and Ctrl+P/K, like the command bar", () => {
    const command = vi.fn<(item: MentionItem) => void>();
    const ref = createRef<MentionListRef>();
    const items: MentionItem[] = [
      { id: "i-1", label: "MUL-1", type: "issue" },
      { id: "i-2", label: "MUL-2", type: "issue" },
      { id: "i-3", label: "MUL-3", type: "issue" },
    ];

    render(
      <I18nWrapper>
        <MentionList ref={ref} items={items} query="" command={command} />
      </I18nWrapper>,
    );

    const highlightedLabel = () => {
      const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>("button"));
      return buttons.find((b) => b.classList.contains("bg-accent"))?.textContent ?? "";
    };
    let handled: boolean | undefined;
    const press = (init: KeyboardEventInit) =>
      act(() => {
        handled = ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", init) });
      });

    expect(highlightedLabel()).toBe("MUL-1");

    press({ key: "n", ctrlKey: true });
    expect(handled).toBe(true);
    expect(highlightedLabel()).toBe("MUL-2");

    press({ key: "j", ctrlKey: true });
    expect(highlightedLabel()).toBe("MUL-3");

    press({ key: "p", ctrlKey: true });
    expect(highlightedLabel()).toBe("MUL-2");

    press({ key: "k", ctrlKey: true });
    expect(highlightedLabel()).toBe("MUL-1");

    // The highlight the aliases moved is the row Enter commits.
    press({ key: "n", ctrlKey: true });
    press({ key: "Enter" });
    expect(command).toHaveBeenCalledTimes(1);
    expect(command.mock.calls[0]?.[0]?.label).toBe("MUL-2");
  });

  // Without Ctrl these letters are ordinary query characters; swallowing them
  // would make "@nick" unsearchable.
  it("leaves bare n/j/p/k to the query instead of moving the highlight", () => {
    const ref = createRef<MentionListRef>();
    const items: MentionItem[] = [
      { id: "i-1", label: "MUL-1", type: "issue" },
      { id: "i-2", label: "MUL-2", type: "issue" },
    ];

    render(
      <I18nWrapper>
        <MentionList ref={ref} items={items} query="" command={vi.fn()} />
      </I18nWrapper>,
    );

    const highlightedLabel = () => {
      const buttons = Array.from(document.querySelectorAll<HTMLButtonElement>("button"));
      return buttons.find((b) => b.classList.contains("bg-accent"))?.textContent ?? "";
    };

    for (const key of ["n", "j", "p", "k"]) {
      let handled: boolean | undefined;
      act(() => {
        handled = ref.current?.onKeyDown({ event: new KeyboardEvent("keydown", { key }) });
      });
      expect(handled).toBe(false);
    }
    expect(highlightedLabel()).toBe("MUL-1");
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
    const result = config.items!(itemArgs("a"));
    const items = result as MentionItem[];

    expect(items.some((i) => i.type === "agent" && i.label === "Athena")).toBe(true);
    expect(items.some((i) => i.type === "agent" && i.label === "Aether")).toBe(true);
    expect(items.some((i) => i.type === "agent" && i.label === "Atlas")).toBe(false);
  });

  it("hides another owner's personal agent from a workspace admin (MUL-3963)", () => {
    // Role lives in the member fixture, not in authState — promoting Alice
    // to admin here exercises the gate. MUL-3963 removed the admin bypass:
    // a private agent is invocable only by its owner, so the @mention list
    // must NOT surface Bob's personal agent to admin Alice.
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
    const result = config.items!(itemArgs("a"));
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
    const result = config.items!(itemArgs("bug"));

    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "issue" && i.id === "i1")).toBe(true);
  });

  it("paints issue suggestions with their custom status color", () => {
    render(
      <I18nWrapper>
        <MentionList
          items={[
            {
              id: "issue-6956",
              label: "MUL-6956",
              type: "issue",
              status: "awaiting_response",
              statusCategory: "in_review",
            },
          ]}
          query=""
          command={vi.fn()}
        />
      </I18nWrapper>,
    );

    const statusIcon = screen
      .getByText("MUL-6956")
      .closest("button")
      ?.querySelector("svg");
    expect(statusIcon).toHaveStyle({ color: "#f97316" });
  });

  it("does not inject current/recent chat context into the normal @ results", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      issues: [{ id: "i1", identifier: "MUL-1", title: "Login bug", status: "todo" }],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs("")) as MentionItem[];

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
    const result = config.items!(itemArgs("")) as MentionItem[];

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
    const result = config.items!(itemArgs("a")) as MentionItem[];

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

  it("localizes agent and squad badges", () => {
    render(
      <ZhI18nWrapper>
        <MentionList
          items={[
            { id: "a1", label: "Aegis", type: "agent" },
            { id: "s1", label: "Core team", type: "squad" },
          ]}
          query=""
          command={vi.fn()}
        />
      </ZhI18nWrapper>,
    );

    expect(screen.getByText("智能体")).toBeInTheDocument();
    expect(screen.getByText("小队")).toBeInTheDocument();
    expect(screen.queryByText("Agent")).not.toBeInTheDocument();
    expect(screen.queryByText("Squad")).not.toBeInTheDocument();
  });

  it("includes squads with a runnable leader in the mention list", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      agents: [
        {
          id: "leader-1",
          name: "Leader",
          archived_at: null,
          visibility: "workspace",
          owner_id: null,
        },
      ],
      squads: [
        {
          id: "s1",
          name: "Jiayuan's Coding Team",
          archived_at: null,
          leader_id: "leader-1",
        },
        {
          id: "s2",
          name: "独立团",
          archived_at: null,
          leader_id: "leader-1",
        },
        {
          id: "s3",
          name: "Archived Squad",
          archived_at: "2026-01-01T00:00:00Z",
          leader_id: "leader-1",
        },
      ],
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs(""));

    const items = result as MentionItem[];
    expect(items.filter((i) => i.type === "squad")).toHaveLength(2);
    expect(items.some((i) => i.type === "squad" && i.label === "Jiayuan's Coding Team")).toBe(true);
    expect(items.some((i) => i.type === "squad" && i.label === "独立团")).toBe(true);
    expect(items.some((i) => i.type === "squad" && i.label === "Archived Squad")).toBe(false);
  });

  it("keeps a squad with an unbound leader discoverable but unselectable", () => {
    const qc = fakeQc({
      agents: [
        {
          id: "leader-1",
          name: "Leader",
          archived_at: null,
          runtime_id: "",
          runtime_bound: false,
          visibility: "workspace",
          owner_id: null,
        },
      ],
      squads: [
        {
          id: "s1",
          name: "Unrunnable Squad",
          archived_at: null,
          leader_id: "leader-1",
        },
      ],
    });

    const config = createMentionSuggestion(qc);
    const items = config.items!(itemArgs("")) as MentionItem[];

    expect(items).toContainEqual(
      expect.objectContaining({
        type: "squad",
        id: "s1",
        disabledReason: "agent_runtime_required",
      }),
    );
  });

  it("keeps squads discoverable while the agents cache is not ready", () => {
    const qc = fakeQc({
      squads: [
        {
          id: "s1",
          name: "Cold Cache Squad",
          archived_at: null,
          leader_id: "leader-not-cached",
        },
      ],
    });

    const config = createMentionSuggestion(qc);
    const items = config.items!(itemArgs("")) as MentionItem[];
    const squad = items.find((item) => item.type === "squad" && item.id === "s1");

    expect(squad).toBeDefined();
    expect(squad?.disabledReason).toBeUndefined();
  });

  it("keeps a squad with an archived leader discoverable", () => {
    const qc = fakeQc({
      agents: [
        {
          id: "leader-1",
          name: "Archived Leader",
          archived_at: "2026-01-01T00:00:00Z",
          visibility: "workspace",
          owner_id: null,
        },
      ],
      squads: [
        {
          id: "s1",
          name: "Archived Leader Squad",
          archived_at: null,
          leader_id: "leader-1",
        },
      ],
    });

    const config = createMentionSuggestion(qc);
    const items = config.items!(itemArgs("")) as MentionItem[];
    const squad = items.find((item) => item.type === "squad" && item.id === "s1");

    expect(squad).toBeDefined();
    expect(squad?.disabledReason).toBeUndefined();
  });

  it("returns no squads when the squads cache is empty (not yet fetched)", () => {
    const qc = fakeQc({
      members: [{ user_id: "u1", name: "Alice", role: "member" }],
      // squads not provided — simulates cache miss
    });
    searchIssuesMock.mockReturnValue(new Promise(() => {}));

    const config = createMentionSuggestion(qc);
    const result = config.items!(itemArgs(""));

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
    const result = config.items!(itemArgs("liyunlong"));

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
    const result = config.items!(itemArgs("lyl"));

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
    const result = config.items!(itemArgs("whs"));

    const items = result as MentionItem[];
    expect(items.some((i) => i.type === "agent" && i.label === "魏和尚")).toBe(true);
  });
});

// MUL-5824: the search API already demotes cancelled issues, but the picker
// merges its local cache AHEAD of the server results and only then truncates to
// MAX_ITEMS — so a cached cancelled row could both outrank and crowd out live
// work the backend had deliberately ranked above it.
describe("MentionList cancelled demotion", () => {
  beforeEach(() => {
    searchIssuesMock.mockReset();
    searchProjectsMock.mockReset();
    searchIssuesMock.mockResolvedValue({ issues: [], total: 0 });
    searchProjectsMock.mockResolvedValue({ projects: [], total: 0 });
  });

  // Rendered top-to-bottom order of the issue rows. textContent runs the
  // identifier straight into the title, so match the identifier off the front.
  const issueLabels = () =>
    Array.from(document.querySelectorAll<HTMLButtonElement>("button"))
      .map((b) => (b.textContent ?? "").match(/^MUL-\d+/)?.[0])
      .filter((label): label is string => !!label);

  it("sorts cancelled issues below live ones regardless of input order", () => {
    const items: MentionItem[] = [
      { id: "i-1", label: "MUL-1", type: "issue", status: "cancelled", statusCategory: "cancelled" },
      { id: "i-2", label: "MUL-2", type: "issue", status: "in_progress" },
      { id: "i-3", label: "MUL-3", type: "issue", status: "cancelled", statusCategory: "cancelled" },
      { id: "i-4", label: "MUL-4", type: "issue", status: "backlog" },
    ];

    render(<I18nWrapper><MentionList items={items} query="" command={vi.fn()} /></I18nWrapper>);

    // Live rows keep their relative order; cancelled rows keep theirs too.
    expect(issueLabels()).toEqual(["MUL-2", "MUL-4", "MUL-1", "MUL-3"]);
  });

  it("gives up the slot, not just the position, when the list overflows", () => {
    // 20 cancelled rows ahead of one live row: with only 20 slots the live row
    // is invisible unless the demotion runs BEFORE the truncation.
    const items: MentionItem[] = [
      ...Array.from({ length: 20 }, (_, n) => ({
        id: `i-c${n}`,
        label: `MUL-${100 + n}`,
        type: "issue" as const,
        status: "cancelled" as const,
        statusCategory: "cancelled" as const,
      })),
      { id: "i-live", label: "MUL-9", type: "issue", status: "todo" },
    ];

    render(<I18nWrapper><MentionList items={items} query="" command={vi.fn()} /></I18nWrapper>);

    const labels = issueLabels();
    expect(labels).toHaveLength(20);
    expect(labels[0]).toBe("MUL-9");
    // One cancelled row was dropped to make room, not the live one.
    expect(labels).not.toContain("MUL-119");
  });

  it("keeps a cancelled issue you are currently viewing in its Current section", () => {
    // "Current" is explicit context, not a relevance hit — demoting it past the
    // truncation would make the issue on screen vanish from its own picker.
    const items: MentionItem[] = [
      { id: "i-cur", label: "MUL-7", type: "issue", status: "cancelled", statusCategory: "cancelled", group: "current" },
      { id: "i-live", label: "MUL-8", type: "issue", status: "in_progress" },
    ];

    render(
      <I18nWrapper>
        <MentionList items={items} query="" command={vi.fn()} includeProjectSearch />
      </I18nWrapper>,
    );

    expect(screen.getByText("Current page")).toBeInTheDocument();
    expect(issueLabels()).toEqual(["MUL-7", "MUL-8"]);
  });

  it("does not reorder server results, which the API already ranked", async () => {
    searchIssuesMock.mockResolvedValue({
      issues: [
        { id: "i-a", identifier: "MUL-11", title: "Live match", status: "todo" },
        { id: "i-b", identifier: "MUL-12", title: "Cancelled match", status: "cancelled" },
      ],
      total: 2,
    });

    render(<I18nWrapper><MentionList items={[]} query="match" command={vi.fn()} /></I18nWrapper>);

    await waitFor(() => {
      expect(screen.getByText("MUL-11")).toBeInTheDocument();
    });
    expect(issueLabels()).toEqual(["MUL-11", "MUL-12"]);
  });

  it("demotes a cached cancelled row below a server-ranked live one", async () => {
    searchIssuesMock.mockResolvedValue({
      issues: [{ id: "i-live", identifier: "MUL-21", title: "Live match", status: "in_progress" }],
      total: 1,
    });

    // The cached row is merged first; without the demotion it would render on
    // top of the server's higher-ranked live match.
    const items: MentionItem[] = [
      { id: "i-cached", label: "MUL-20", type: "issue", status: "cancelled", statusCategory: "cancelled", description: "Cancelled match" },
    ];

    render(<I18nWrapper><MentionList items={items} query="match" command={vi.fn()} /></I18nWrapper>);

    await waitFor(() => {
      expect(screen.getByText("MUL-21")).toBeInTheDocument();
    });
    expect(issueLabels()).toEqual(["MUL-21", "MUL-20"]);
  });

  // Cross-type half of MUL-5824. Context mentions aggregate two independently
  // ranked responses — issues then projects, both tagged `search` — so per-type
  // ranking alone left a cancelled project above a live issue, and cancelled
  // search rows were exempted from the client partition entirely.
  describe("mixed issue/project results", () => {
    // Rendered order of every result row: issue rows put their identifier in
    // the leading font-medium span, project rows their title.
    const rowLabels = () =>
      Array.from(document.querySelectorAll<HTMLButtonElement>("button"))
        .map((b) => b.querySelector("span.font-medium")?.textContent ?? "")
        .filter((label) => label !== "");

    const headings = () =>
      Array.from(
        document.querySelectorAll<HTMLElement>("div.uppercase"),
      ).map((el) => el.textContent ?? "");

    it("keeps a cancelled project below a live issue in the search results", async () => {
      searchIssuesMock.mockResolvedValue({
        issues: [
          { id: "i-live", identifier: "MUL-31", title: "Live issue", status: "todo" },
        ],
        total: 1,
      });
      searchProjectsMock.mockResolvedValue({
        projects: [
          { id: "p-dead", title: "Dead project", description: null, icon: null, status: "cancelled" },
        ],
        total: 1,
      });

      render(
        <I18nWrapper>
          <MentionList items={[]} query="thing" command={vi.fn()} includeProjectSearch />
        </I18nWrapper>,
      );

      await waitFor(() => {
        expect(screen.getByText("Dead project")).toBeInTheDocument();
      });
      expect(rowLabels()).toEqual(["MUL-31", "Dead project"]);
      expect(headings()).toEqual(["Search results", "Cancelled"]);
    });

    it("demotes cancelled search results of both types below every live row", async () => {
      searchIssuesMock.mockResolvedValue({
        issues: [
          { id: "i-dead", identifier: "MUL-41", title: "Dead issue", status: "cancelled" },
          { id: "i-live", identifier: "MUL-42", title: "Live issue", status: "in_review" },
        ],
        total: 2,
      });
      searchProjectsMock.mockResolvedValue({
        projects: [
          { id: "p-dead", title: "Dead project", description: null, icon: null, status: "cancelled" },
          { id: "p-live", title: "Live project", description: null, icon: null, status: "planned" },
        ],
        total: 2,
      });

      render(
        <I18nWrapper>
          <MentionList items={[]} query="thing" command={vi.fn()} includeProjectSearch />
        </I18nWrapper>,
      );

      await waitFor(() => {
        expect(screen.getByText("Live project")).toBeInTheDocument();
      });
      // Live rows keep their server order; both cancelled types land last.
      expect(rowLabels()).toEqual([
        "MUL-42",
        "Live project",
        "MUL-41",
        "Dead project",
      ]);
    });

    it("keeps a cancelled project you are currently viewing in its Current section", async () => {
      searchIssuesMock.mockResolvedValue({ issues: [], total: 0 });
      searchProjectsMock.mockResolvedValue({ projects: [], total: 0 });

      const items: MentionItem[] = [
        {
          id: "p-cur",
          label: "Current project",
          type: "project",
          projectStatus: "cancelled",
          group: "current",
        },
        { id: "i-live", label: "MUL-51", type: "issue", status: "todo" },
      ];

      render(
        <I18nWrapper>
          <MentionList items={items} query="" command={vi.fn()} includeProjectSearch />
        </I18nWrapper>,
      );

      expect(rowLabels()).toEqual(["Current project", "MUL-51"]);
      expect(headings()).toEqual(["Current page", "Issues"]);
    });

    it("gives up slots to live rows across both types when the list overflows", async () => {
      // 20 cancelled projects ahead of one live issue: with only 20 slots the
      // live row is invisible unless the cross-type demotion runs BEFORE the
      // truncation.
      searchIssuesMock.mockResolvedValue({ issues: [], total: 0 });
      searchProjectsMock.mockResolvedValue({ projects: [], total: 0 });

      const items: MentionItem[] = [
        ...Array.from({ length: 20 }, (_, n) => ({
          id: `p-c${n}`,
          label: `Dead project ${n}`,
          type: "project" as const,
          projectStatus: "cancelled" as const,
        })),
        { id: "i-live", label: "MUL-61", type: "issue", status: "todo" },
      ];

      render(
        <I18nWrapper>
          <MentionList items={items} query="" command={vi.fn()} includeProjectSearch />
        </I18nWrapper>,
      );

      const labels = rowLabels();
      expect(labels).toHaveLength(20);
      expect(labels[0]).toBe("MUL-61");
      // One cancelled project was dropped to make room, not the live issue.
      expect(labels).not.toContain("Dead project 19");
    });
  });

  // A direct hit — exact identifier, bare number, or full title — is the record
  // the user typed out in full. The backend ranks it first and exempts it from
  // the cancelled demotion; the picker has to agree, and exemption alone is not
  // enough: slice(0, MAX_ITEMS) runs on the merged list, so a direct hit sitting
  // behind a full window of cached candidates would still be cut.
  describe("direct hits", () => {
    const rowLabels = () =>
      Array.from(document.querySelectorAll<HTMLButtonElement>("button"))
        .map((b) => b.querySelector("span.font-medium")?.textContent ?? "")
        .filter((label) => label !== "");

    const headings = () =>
      Array.from(
        document.querySelectorAll<HTMLElement>("div.uppercase"),
      ).map((el) => el.textContent ?? "");

    it("keeps a cancelled issue matched by exact identifier out of the Cancelled group", async () => {
      searchIssuesMock.mockResolvedValue({
        issues: [
          { id: "i-hit", identifier: "MUL-77", title: "Abandoned plan", status: "cancelled" },
          { id: "i-other", identifier: "MUL-78", title: "MUL-77 follow-up", status: "todo" },
        ],
        total: 2,
      });

      render(<I18nWrapper><MentionList items={[]} query="MUL-77" command={vi.fn()} /></I18nWrapper>);

      await waitFor(() => {
        expect(screen.getByText("MUL-77")).toBeInTheDocument();
      });
      // The target the user spelled out stays on top; no demotion, no section.
      expect(rowLabels()).toEqual(["MUL-77", "MUL-78"]);
      expect(headings()).not.toContain("Cancelled");
    });

    it("treats a bare number as targeting the issue with that number", async () => {
      searchIssuesMock.mockResolvedValue({
        issues: [
          { id: "i-live", identifier: "MUL-800", title: "Mentions 77 in passing", status: "todo" },
          { id: "i-hit", identifier: "MUL-77", title: "Abandoned plan", status: "cancelled" },
        ],
        total: 2,
      });

      render(<I18nWrapper><MentionList items={[]} query="77" command={vi.fn()} /></I18nWrapper>);

      await waitFor(() => {
        expect(screen.getByText("MUL-77")).toBeInTheDocument();
      });
      expect(rowLabels()).toEqual(["MUL-77", "MUL-800"]);
    });

    it("keeps a cancelled project matched by its full title with the live rows", async () => {
      searchIssuesMock.mockResolvedValue({
        issues: [{ id: "i-live", identifier: "MUL-81", title: "Search revamp notes", status: "todo" }],
        total: 1,
      });
      searchProjectsMock.mockResolvedValue({
        projects: [
          { id: "p-hit", title: "Search revamp", description: null, icon: null, status: "cancelled" },
        ],
        total: 1,
      });

      render(
        <I18nWrapper>
          <MentionList items={[]} query="Search revamp" command={vi.fn()} includeProjectSearch />
        </I18nWrapper>,
      );

      await waitFor(() => {
        expect(screen.getByText("Search revamp")).toBeInTheDocument();
      });
      expect(rowLabels()).toEqual(["Search revamp", "MUL-81"]);
      expect(headings()).not.toContain("Cancelled");
    });

    it("keeps a cancelled direct hit visible behind a full window of live candidates", async () => {
      // 20 non-cancelled cached candidates already fill every slot. Exempting
      // the direct hit from the demotion would leave it at position 21 and the
      // truncation would still delete it, so it has to be pinned to the front.
      searchIssuesMock.mockResolvedValue({
        issues: [
          { id: "i-hit", identifier: "MUL-99", title: "Abandoned plan", status: "cancelled" },
        ],
        total: 1,
      });

      const items: MentionItem[] = Array.from({ length: 20 }, (_, n) => ({
        id: `i-live${n}`,
        label: `MUL-${200 + n}`,
        type: "issue" as const,
        status: "todo" as const,
      }));

      render(<I18nWrapper><MentionList items={items} query="MUL-99" command={vi.fn()} /></I18nWrapper>);

      await waitFor(() => {
        expect(screen.getByText("MUL-99")).toBeInTheDocument();
      });

      const labels = rowLabels();
      expect(labels).toHaveLength(20);
      expect(labels[0]).toBe("MUL-99");
      // A live candidate gave up the last slot, not the record the user typed.
      expect(labels).not.toContain("MUL-219");
    });

    it("still demotes a cancelled row that merely contains the query", async () => {
      // Guard against over-exempting: a partial match is not a direct hit.
      searchIssuesMock.mockResolvedValue({
        issues: [
          { id: "i-dead", identifier: "MUL-91", title: "Abandoned plan", status: "cancelled" },
          { id: "i-live", identifier: "MUL-92", title: "Active plan", status: "todo" },
        ],
        total: 2,
      });

      render(<I18nWrapper><MentionList items={[]} query="plan" command={vi.fn()} /></I18nWrapper>);

      await waitFor(() => {
        expect(screen.getByText("MUL-92")).toBeInTheDocument();
      });
      expect(rowLabels()).toEqual(["MUL-92", "MUL-91"]);
      expect(headings()).toContain("Cancelled");
    });
  });
});
