import { act, type ReactNode } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import { WORKSPACE_PAGES } from "@multica/core/paths";
import { SearchCommand } from "./search-command";
import { useSearchStore } from "./search-store";
import enCommon from "../locales/en/common.json";
import enAuth from "../locales/en/auth.json";
import enSettings from "../locales/en/settings.json";
import enSearch from "../locales/en/search.json";
// The palette labels its Pages group from the sidebar's own nav strings, so
// the layout namespace is part of its contract, not incidental setup.
import enLayout from "../locales/en/layout.json";
import enProjects from "../locales/en/projects.json";
import zhHansProjects from "../locales/zh-Hans/projects.json";

const TEST_RESOURCES = {
  en: {
    common: enCommon,
    auth: enAuth,
    settings: enSettings,
    search: enSearch,
    layout: enLayout,
    projects: enProjects,
  },
};

// Deliberately NOT a full zh-Hans bundle: only `projects` is translated, and
// every other namespace stays on its English bundle under the zh-Hans key.
// The one thing under test is whether a project row names its status through
// the projects namespace, and keeping the chrome in English lets these tests
// go on addressing the palette by its English placeholder and group headings.
const ZH_TEST_RESOURCES = {
  "zh-Hans": {
    common: enCommon,
    auth: enAuth,
    settings: enSettings,
    search: enSearch,
    layout: enLayout,
    projects: zhHansProjects,
  },
};

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const renderSearch = () => render(<SearchCommand />, { wrapper: I18nWrapper });

function ChineseI18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="zh-Hans" resources={ZH_TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const renderSearchInChinese = () =>
  render(<SearchCommand />, { wrapper: ChineseI18nWrapper });

const {
  mockPush,
  mockSearchIssues,
  mockSearchProjects,
  mockRecentItems,
  mockAllIssues,
  mockSetTheme,
  mockTheme,
  mockPathname,
  mockGetShareableUrl,
  mockMembers,
  mockAgents,
  mockSquads,
  mockOpenModal,
  mockToastSuccess,
  mockClipboardWrite,
  mockTimeline,
  mockCommentCollapseAll,
  mockCommentExpandAll,
  mockResolvedCollapseAll,
  mockResolvedExpandAll,
} = vi.hoisted(() => ({
  mockPush: vi.fn(),
  mockSearchIssues: vi.fn(),
  mockSearchProjects: vi.fn(),
  mockRecentItems: { current: [] as Array<{ id: string; visitedAt: number }> },
  mockAllIssues: { current: [] as Array<Record<string, unknown>> },
  mockSetTheme: vi.fn(),
  mockTheme: { current: "system" as "light" | "dark" | "system" },
  mockPathname: { current: "/ws-test/issues" as string },
  mockGetShareableUrl: vi.fn((p: string) => `https://app.multica/${p}`),
  mockMembers: {
    current: [] as Array<{
      id: string;
      workspace_id: string;
      user_id: string;
      role: "owner" | "admin" | "member";
      created_at: string;
      name: string;
      email: string;
      avatar_url: string | null;
    }>,
  },
  mockAgents: {
    current: [] as Array<{
      id: string;
      name: string;
      avatar_url: string | null;
    }>,
  },
  mockSquads: {
    current: [] as Array<{
      id: string;
      name: string;
      avatar_url: string | null;
    }>,
  },
  mockOpenModal: vi.fn(),
  mockToastSuccess: vi.fn(),
  mockClipboardWrite: vi.fn(() => Promise.resolve()),
  mockTimeline: { current: [] as Array<Record<string, unknown>> },
  mockCommentCollapseAll: vi.fn(),
  mockCommentExpandAll: vi.fn(),
  mockResolvedCollapseAll: vi.fn(),
  mockResolvedExpandAll: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getBaseUrl: () => "http://127.0.0.1:8080",
    searchIssues: mockSearchIssues,
    searchProjects: mockSearchProjects,
  },
}));

vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: ({
    actorType,
    actorId,
  }: {
    actorType: string;
    actorId: string;
  }) => {
    const name =
      actorType === "member"
        ? mockMembers.current.find((m) => m.user_id === actorId)?.name
        : actorType === "agent"
          ? mockAgents.current.find((a) => a.id === actorId)?.name
          : actorType === "squad"
            ? mockSquads.current.find((s) => s.id === actorId)?.name
            : undefined;
    return (
      <span
        data-testid="issue-assignee-avatar"
        title={name ?? `${actorType}:${actorId}`}
      />
    );
  },
}));

vi.mock("@multica/core/issues/stores", () => {
  const EMPTY: Array<{ id: string; visitedAt: number }> = [];
  return {
    useRecentIssuesStore: (
      selector?: (state: {
        byWorkspace: Record<string, typeof mockRecentItems.current>;
      }) => unknown,
    ) => {
      const state = { byWorkspace: { "ws-test": mockRecentItems.current } };
      return selector ? selector(state) : state;
    },
    selectRecentIssues:
      (wsId: string | null) =>
      (state: { byWorkspace: Record<string, typeof mockRecentItems.current> }) =>
        wsId ? (state.byWorkspace[wsId] ?? EMPTY) : EMPTY,
    openCreateIssueWithPreference: (data?: Record<string, unknown> | null) =>
      mockOpenModal("quick-create-issue", data ?? null),
    useCommentCollapseStore: Object.assign(vi.fn(), {
      getState: () => ({
        collapseAll: mockCommentCollapseAll,
        expandAll: mockCommentExpandAll,
      }),
    }),
    useResolvedExpandStore: Object.assign(vi.fn(), {
      getState: () => ({
        collapseAll: mockResolvedCollapseAll,
        expandAll: mockResolvedExpandAll,
      }),
    }),
  };
});

vi.mock("@multica/core", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  // Spread the real module so pure helpers (resolveRouteIconName, used to
  // derive each nav page's icon from its href) stay intact.
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    inbox: () => "/ws-test/inbox",
    chat: () => "/ws-test/chat",
    myIssues: () => "/ws-test/my-issues",
    issues: () => "/ws-test/issues",
    projects: () => "/ws-test/projects",
    autopilots: () => "/ws-test/autopilots",
    agents: () => "/ws-test/agents",
    squads: () => "/ws-test/squads",
    usage: () => "/ws-test/usage",
    runtimes: () => "/ws-test/runtimes",
    skills: () => "/ws-test/skills",
    settings: () => "/ws-test/settings",
    issueDetail: (id: string) => `/ws-test/issues/${id}`,
    memberDetail: (id: string) => `/ws-test/members/${id}`,
    agentDetail: (id: string) => `/ws-test/agents/${id}`,
    squadDetail: (id: string) => `/ws-test/squads/${id}`,
    projectDetail: (id: string) => `/ws-test/projects/${id}`,
  }),
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_wsId: string, id: string) => ({
    queryKey: ["issues", "ws-test", "detail", id],
  }),
  issueTimelineOptions: (id: string) => ({
    queryKey: ["issues", "timeline", id],
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["workspaces", "ws-test", "members"] }),
  agentListOptions: () => ({ queryKey: ["workspaces", "ws-test", "agents"] }),
  squadListOptions: () => ({ queryKey: ["workspaces", "ws-test", "squads"] }),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(vi.fn(), {
    getState: () => ({ open: mockOpenModal }),
  }),
}));

function resolveIssue(key: readonly unknown[]) {
  // issueDetailOptions key shape: ["issues", wsId, "detail", id]
  if (key[0] === "issues" && key[2] === "detail") {
    const id = key[3];
    return mockAllIssues.current.find((i) => i.id === id);
  }
  return undefined;
}

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: readonly unknown[]; enabled?: boolean }) => {
    const key = opts.queryKey;
    if (key[0] === "workspaces" && key[2] === "members") {
      return { data: mockMembers.current };
    }
    if (key[0] === "workspaces" && key[2] === "agents") {
      return { data: mockAgents.current };
    }
    if (key[0] === "workspaces" && key[2] === "squads") {
      return { data: mockSquads.current };
    }
    if (opts.enabled === false) return { data: undefined };
    return { data: resolveIssue(key) };
  },
  useQueries: (opts: { queries: Array<{ queryKey: readonly unknown[] }> }) =>
    opts.queries.map((q) => ({ data: resolveIssue(q.queryKey) })),
  useQueryClient: () => ({
    ensureQueryData: (opts: { queryKey: readonly unknown[] }) =>
      opts.queryKey[1] === "timeline"
        ? Promise.resolve(mockTimeline.current)
        : Promise.reject(new Error(`unexpected key ${String(opts.queryKey)}`)),
  }),
}));

// Mock the context module, not the barrel: resolveClickIntent and
// useIntentNavigate stay the REAL implementations and read this adapter
// (no openInNewTab — the web shape, so tab intents go through window.open).
vi.mock("../navigation/context", () => {
  const adapter = () => ({
    push: mockPush,
    pathname: mockPathname.current,
    getShareableUrl: mockGetShareableUrl,
  });
  return {
    useNavigation: adapter,
    useOptionalNavigation: adapter,
  };
});

vi.mock("@multica/ui/components/common/theme-provider", () => ({
  useTheme: () => ({ theme: mockTheme.current, setTheme: mockSetTheme }),
}));

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: vi.fn() },
}));

describe("SearchCommand", () => {
  beforeEach(() => {
    mockPush.mockReset();
    mockSearchIssues.mockReset().mockResolvedValue({ issues: [] });
    mockSearchProjects.mockReset().mockResolvedValue({ projects: [] });
    mockRecentItems.current = [];
    mockAllIssues.current = [];
    mockAgents.current = [];
    mockSquads.current = [];
    mockSetTheme.mockReset();
    mockTheme.current = "system";
    mockPathname.current = "/ws-test/issues";
    mockGetShareableUrl.mockReset().mockImplementation((p: string) => `https://app.multica/${p}`);
    mockMembers.current = [];
    mockOpenModal.mockReset();
    mockToastSuccess.mockReset();
    mockClipboardWrite.mockReset().mockResolvedValue(undefined);
    mockTimeline.current = [];
    mockCommentCollapseAll.mockReset();
    mockCommentExpandAll.mockReset();
    mockResolvedCollapseAll.mockReset();
    mockResolvedExpandAll.mockReset();

    // cmdk calls scrollIntoView on the first selected item, which jsdom doesn't implement
    Element.prototype.scrollIntoView = vi.fn();

    act(() => {
      useSearchStore.setState({ open: true });
    });
  });

  it("closes on a single Escape press from the search input", async () => {
    const user = userEvent.setup();

    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.click(input);

    expect(useSearchStore.getState().open).toBe(true);

    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(useSearchStore.getState().open).toBe(false);
    });
    expect(screen.queryByPlaceholderText("Type a command or search...")).not.toBeInTheDocument();
  });

  it("shows only New Issue by default and hides Pages / low-frequency commands until query", () => {
    renderSearch();

    expect(screen.queryByText("Pages")).not.toBeInTheDocument();
    // Only the primary creation action surfaces on empty query; everything
    // else (theme, copy, New Project) must be revealed by typing.
    expect(screen.getByText("Commands")).toBeInTheDocument();
    expect(
      screen.getByText((_, el) => el?.textContent === "New Issue" && el?.tagName === "SPAN"),
    ).toBeInTheDocument();
    expect(screen.queryByText("New Project")).not.toBeInTheDocument();
    expect(screen.queryByText("Switch to Light Theme")).not.toBeInTheDocument();
    expect(screen.queryByText("Switch to Dark Theme")).not.toBeInTheDocument();
    expect(screen.queryByText("Use System Theme")).not.toBeInTheDocument();
  });

  it("filters navigation pages by query", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "set");

    await waitFor(() => {
      // HighlightText splits text, so use a function matcher
      expect(screen.getByText((_, el) => el?.textContent === "Settings" && el?.tagName === "SPAN")).toBeInTheDocument();
    });
    expect(screen.queryByText("Inbox")).not.toBeInTheDocument();
  });

  it("offers every workspace nav page, not a hand-maintained subset", async () => {
    const user = userEvent.setup();
    renderSearch();
    const input = screen.getByPlaceholderText("Type a command or search...");

    // The Pages group is generated from WORKSPACE_PAGES — the same registry
    // the sidebar and the desktop tab bar read — so searching a page by the
    // exact name the sidebar shows must always reach it. The hand-written
    // list this replaced had gone stale by four pages (MUL-6272).
    for (const page of Object.values(WORKSPACE_PAGES)) {
      const label = enLayout.nav[page.navKey];
      await user.clear(input);
      await user.type(input, label);
      expect(
        await screen.findByText(
          (_, el) => el?.textContent === label && el?.tagName === "SPAN",
        ),
      ).toBeInTheDocument();
    }
  });

  it("does not surface a page on an incidental substring of a hidden keyword", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "s");

    // Inbox carries the hidden keyword "notifications". Matching keywords by
    // substring made a bare "s" pull Inbox into the list with nothing on the
    // row to explain why; keywords match by prefix instead.
    await waitFor(() => {
      expect(
        screen.getByText((_, el) => el?.textContent === "Settings" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });
    expect(screen.queryByText("Inbox")).not.toBeInTheDocument();
  });

  it("navigates to a page whose label differs from its route segment", async () => {
    const user = userEvent.setup();
    renderSearch();

    // Analytics lives at /usage: proof the row resolves its destination from
    // the page key rather than from the words on screen.
    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "analytics");

    await user.click(await screen.findByText("Analytics"));

    expect(mockPush).toHaveBeenCalledWith("/ws-test/usage");
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("navigates to page on selection", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "settings");

    const settingsItem = await screen.findByText("Settings");
    await user.click(settingsItem);

    expect(mockPush).toHaveBeenCalledWith("/ws-test/settings");
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("cmd-click on a result opens it in a new tab instead of navigating in place", async () => {
    const user = userEvent.setup();
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "settings");

    const settingsItem = await screen.findByText("Settings");
    fireEvent.click(settingsItem, { metaKey: true });

    expect(open).toHaveBeenCalledWith(
      "https://app.multica//ws-test/settings",
      "_blank",
      "noopener,noreferrer",
    );
    expect(mockPush).not.toHaveBeenCalled();
    expect(useSearchStore.getState().open).toBe(false);
    open.mockRestore();
  });

  it("cmd+Enter opens the highlighted result in a new tab", async () => {
    const user = userEvent.setup();
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "settings");
    await screen.findByText("Settings");
    fireEvent.keyDown(input, { key: "Enter", metaKey: true });

    expect(open).toHaveBeenCalledWith(
      "https://app.multica//ws-test/settings",
      "_blank",
      "noopener,noreferrer",
    );
    expect(mockPush).not.toHaveBeenCalled();
    open.mockRestore();
  });

  it("a plain selection after an abandoned cmd-click does not inherit the stale intent", async () => {
    const user = userEvent.setup();
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    // cmd-click somewhere that selects nothing…
    fireEvent.click(input, { metaKey: true });
    await user.type(input, "settings");

    const settingsItem = await screen.findByText("Settings");
    // …then a plain click must navigate in place, not open a tab.
    await user.click(settingsItem);

    expect(mockPush).toHaveBeenCalledWith("/ws-test/settings");
    expect(open).not.toHaveBeenCalled();
    open.mockRestore();
  });

  it("lists workspace members and navigates to the member page on selection", async () => {
    const user = userEvent.setup();
    mockMembers.current = [
      {
        id: "member-1",
        workspace_id: "ws-test",
        user_id: "user-1",
        role: "member",
        created_at: "2026-01-01T00:00:00Z",
        name: "Alice Zhang",
        email: "alice@example.com",
        avatar_url: null,
      },
      {
        id: "member-2",
        workspace_id: "ws-test",
        user_id: "user-2",
        role: "admin",
        created_at: "2026-01-01T00:00:00Z",
        name: "Bob Liu",
        email: "bob@example.com",
        avatar_url: null,
      },
    ];
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "alice");

    await waitFor(() => {
      expect(screen.getByText("Members")).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Alice Zhang" && el?.tagName === "DIV"),
      ).toBeInTheDocument();
    });
    expect(
      screen.getByText((_, el) => el?.textContent === "alice@example.com" && el?.tagName === "DIV"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Bob Liu")).not.toBeInTheDocument();

    const aliceItem = await screen.findByText(
      (_, el) => el?.textContent === "Alice Zhang" && el?.tagName === "DIV",
    );
    await user.click(aliceItem);

    expect(mockPush).toHaveBeenCalledWith("/ws-test/members/user-1");
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("renders recent issues from query cache joined with store visit records", () => {
    mockRecentItems.current = [
      { id: "issue-1", visitedAt: 1000 },
      { id: "issue-2", visitedAt: 900 },
    ];
    mockAllIssues.current = [
      { id: "issue-1", identifier: "MUL-1", title: "First issue", status: "todo" },
      { id: "issue-2", identifier: "MUL-2", title: "Second issue", status: "done" },
    ];

    renderSearch();

    expect(screen.getByText("Recent")).toBeInTheDocument();
    expect(screen.getByText("First issue")).toBeInTheDocument();
    expect(screen.getByText("MUL-1")).toBeInTheDocument();
    expect(screen.getByText("Second issue")).toBeInTheDocument();
    expect(screen.getByText("MUL-2")).toBeInTheDocument();
  });

  it("shows New Issue / New Project under Commands and triggers the modal store", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "new");

    await waitFor(() => {
      expect(screen.getByText("Commands")).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "New Issue" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "New Project" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });

    const newIssue = await screen.findByText(
      (_, el) => el?.textContent === "New Issue" && el?.tagName === "SPAN",
    );
    await user.click(newIssue);

    expect(mockOpenModal).toHaveBeenCalledWith("quick-create-issue", null);
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("hides copy-link commands when not on an issue detail route", async () => {
    const user = userEvent.setup();
    mockPathname.current = "/ws-test/projects";
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "copy");

    // Commands section may still be empty / absent.
    expect(screen.queryByText("Copy Issue Link")).not.toBeInTheDocument();
  });

  it("copies issue link and identifier when on an issue detail route", async () => {
    const user = userEvent.setup();
    // userEvent.setup() installs its own navigator.clipboard; spy on it so we
    // intercept the writeText call without clobbering userEvent's internals.
    const writeSpy = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockImplementation(mockClipboardWrite);
    mockPathname.current = "/ws-test/issues/issue-1";
    mockAllIssues.current = [
      { id: "issue-1", identifier: "MUL-42", title: "Demo", status: "todo" },
    ];
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "copy");

    const linkItem = await screen.findByText(
      (_, el) => el?.textContent === "Copy Issue Link" && el?.tagName === "SPAN",
    );
    await user.click(linkItem);

    expect(mockGetShareableUrl).toHaveBeenCalledWith("/ws-test/issues/issue-1");
    expect(mockClipboardWrite).toHaveBeenCalledWith("https://app.multica//ws-test/issues/issue-1");
    expect(mockToastSuccess).toHaveBeenCalledWith("Link copied");

    // Reopen palette and test identifier copy
    act(() => {
      useSearchStore.setState({ open: true });
    });
    const input2 = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input2, "copy");
    const idItem = await screen.findByText(
      (_, el) =>
        el?.textContent === "Copy Identifier (MUL-42)" && el?.tagName === "SPAN",
    );
    await user.click(idItem);
    expect(mockClipboardWrite).toHaveBeenCalledWith("MUL-42");
    expect(mockToastSuccess).toHaveBeenCalledWith("Copied MUL-42");

    writeSpy.mockRestore();
  });

  it("hides fold/unfold-all-comments commands off issue detail routes", async () => {
    const user = userEvent.setup();
    mockPathname.current = "/ws-test/projects";
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "fold");

    expect(screen.queryByText("Fold All Comments")).not.toBeInTheDocument();
    expect(screen.queryByText("Unfold All Comments")).not.toBeInTheDocument();
  });

  it("folds all comments on the current issue, including expanded resolved threads", async () => {
    const user = userEvent.setup();
    mockPathname.current = "/ws-test/issues/issue-1";
    mockAllIssues.current = [
      { id: "issue-1", identifier: "MUL-42", title: "Demo", status: "todo" },
    ];
    mockTimeline.current = [
      { type: "activity", id: "act-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T00:00:00Z", action: "status_changed" },
      { type: "comment", id: "root-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T01:00:00Z", parent_id: null },
      { type: "comment", id: "reply-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T02:00:00Z", parent_id: "root-1" },
      { type: "comment", id: "root-2", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T03:00:00Z", parent_id: null, resolved_at: "2026-01-02T00:00:00Z" },
    ];
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "fold");

    const foldItem = await screen.findByText(
      (_, el) => el?.textContent === "Fold All Comments" && el?.tagName === "SPAN",
    );
    await user.click(foldItem);

    await waitFor(() => {
      // Root comments only — replies fold with their thread, activities are not comments.
      expect(mockCommentCollapseAll).toHaveBeenCalledWith("issue-1", ["root-1", "root-2"]);
    });
    expect(mockResolvedCollapseAll).toHaveBeenCalledWith("issue-1");
    expect(mockCommentExpandAll).not.toHaveBeenCalled();
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("unfolds all comments and expands resolved threads", async () => {
    const user = userEvent.setup();
    mockPathname.current = "/ws-test/issues/issue-1";
    mockAllIssues.current = [
      { id: "issue-1", identifier: "MUL-42", title: "Demo", status: "todo" },
    ];
    mockTimeline.current = [
      { type: "comment", id: "root-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T01:00:00Z", parent_id: null },
      { type: "comment", id: "root-2", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T03:00:00Z", parent_id: null, resolved_at: "2026-01-02T00:00:00Z" },
      // root-3 is reply-resolved: the resolution lives on the reply.
      { type: "comment", id: "root-3", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T04:00:00Z", parent_id: null },
      { type: "comment", id: "reply-3", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T05:00:00Z", parent_id: "root-3", resolved_at: "2026-01-02T01:00:00Z" },
    ];
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "unfold");

    const unfoldItem = await screen.findByText(
      (_, el) => el?.textContent === "Unfold All Comments" && el?.tagName === "SPAN",
    );
    await user.click(unfoldItem);

    await waitFor(() => {
      expect(mockCommentExpandAll).toHaveBeenCalledWith("issue-1");
    });
    // Only threads carrying a resolution get seeded into the expand set.
    expect(mockResolvedExpandAll).toHaveBeenCalledWith("issue-1", ["root-2", "root-3"]);
    expect(mockCommentCollapseAll).not.toHaveBeenCalled();
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("filters theme commands by query keywords", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "dark");

    await waitFor(() => {
      expect(screen.getByText("Commands")).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Switch to Dark Theme" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });
    expect(screen.queryByText("Switch to Light Theme")).not.toBeInTheDocument();
    expect(screen.queryByText("Use System Theme")).not.toBeInTheDocument();
  });

  it("applies the selected theme and closes the palette", async () => {
    const user = userEvent.setup();
    mockTheme.current = "light";
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "dark");

    const darkItem = await screen.findByText(
      (_, el) => el?.textContent === "Switch to Dark Theme" && el?.tagName === "SPAN",
    );
    await user.click(darkItem);

    expect(mockSetTheme).toHaveBeenCalledWith("dark");
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("matches theme action via generic 'theme' keyword and marks current theme", async () => {
    const user = userEvent.setup();
    mockTheme.current = "dark";
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "theme");

    await waitFor(() => {
      expect(
        screen.getByText((_, el) => el?.textContent === "Switch to Light Theme" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Switch to Dark Theme" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Use System Theme" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });
    expect(screen.getByLabelText("Current theme")).toBeInTheDocument();
  });

  it("filters out recent items not present in query cache", () => {
    mockRecentItems.current = [
      { id: "issue-1", visitedAt: 1000 },
      { id: "deleted-issue", visitedAt: 900 },
    ];
    mockAllIssues.current = [
      { id: "issue-1", identifier: "MUL-1", title: "Existing issue", status: "in_progress" },
    ];

    renderSearch();

    expect(screen.getByText("Recent")).toBeInTheDocument();
    expect(screen.getByText("Existing issue")).toBeInTheDocument();
    expect(screen.queryByText("deleted-issue")).not.toBeInTheDocument();
  });

  it("shows the assignee avatar instead of status text for issue search results", async () => {
    const user = userEvent.setup();
    mockMembers.current = [
      {
        id: "member-1",
        workspace_id: "ws-test",
        user_id: "user-1",
        role: "member",
        created_at: "2026-01-01T00:00:00Z",
        name: "Alice Zhang",
        email: "alice@example.com",
        avatar_url: null,
      },
    ];
    mockSearchIssues.mockResolvedValue({
      issues: [
        {
          id: "issue-assigned",
          workspace_id: "ws-test",
          number: 101,
          identifier: "MUL-101",
          title: "Assigned search result",
          description: null,
          status: "in_review",
          priority: "none",
          assignee_type: "member",
          assignee_id: "user-1",
          creator_type: "member",
          creator_id: "user-1",
          parent_issue_id: null,
          project_id: null,
          position: 0,
          start_date: null,
          due_date: null,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
          match_source: "title",
        },
      ],
      total: 1,
    });

    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "assigned");

    await waitFor(
      () => {
        expect(
          screen.getByText((_, el) =>
            el?.textContent === "Assigned search result" &&
            el?.tagName === "SPAN",
          ),
        ).toBeInTheDocument();
      },
      { timeout: 2000 },
    );

    expect(screen.getByTitle("Alice Zhang")).toBeInTheDocument();
    expect(screen.queryByText("In Review")).not.toBeInTheDocument();
  });

  it("shows the assignee avatar instead of status text for recent issues", () => {
    mockRecentItems.current = [{ id: "issue-1", visitedAt: 1000 }];
    mockAgents.current = [{ id: "agent-1", name: "Niko", avatar_url: null }];
    mockAllIssues.current = [
      {
        id: "issue-1",
        identifier: "MUL-1",
        title: "Recent assigned issue",
        status: "done",
        assignee_type: "agent",
        assignee_id: "agent-1",
      },
    ];

    renderSearch();

    expect(screen.getByText("Recent assigned issue")).toBeInTheDocument();
    expect(screen.getByTitle("Niko")).toBeInTheDocument();
    expect(screen.queryByText("Done")).not.toBeInTheDocument();
  });

  it("renders description and comment snippets regardless of match_source", async () => {
    const user = userEvent.setup();
    mockSearchIssues.mockResolvedValue({
      issues: [
        {
          id: "issue-snippet",
          workspace_id: "ws-test",
          number: 99,
          identifier: "MUL-99",
          title: "HTML rendering pipeline",
          description: null,
          status: "todo",
          priority: "none",
          assignee_type: null,
          assignee_id: null,
          creator_type: "member",
          creator_id: "user-1",
          parent_issue_id: null,
          project_id: null,
          position: 0,
          start_date: null,
          due_date: null,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
          match_source: "title",
          matched_description_snippet: "...uses HTML templates for rendering...",
          matched_comment_snippet: "...we should migrate away from HTML...",
        },
      ],
      total: 1,
    });
    renderSearch();

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "html");

    await waitFor(
      () => {
        expect(screen.getByText((_, el) => el?.textContent === "HTML rendering pipeline" && el?.tagName === "SPAN")).toBeInTheDocument();
      },
      { timeout: 2000 },
    );

    // Description snippet should render even though match_source is "title"
    expect(
      screen.getByText((_, el) =>
        (el?.textContent?.includes("uses HTML templates for rendering") ?? false) &&
        el?.tagName === "SPAN",
      ),
    ).toBeInTheDocument();

    // Comment snippet should render even though match_source is "title"
    expect(
      screen.getByText((_, el) =>
        (el?.textContent?.includes("we should migrate away from HTML") ?? false) &&
        el?.tagName === "SPAN",
      ),
    ).toBeInTheDocument();
  });

  it("lets Home/End edit the query caret instead of hijacking result navigation", async () => {
    const user = userEvent.setup();
    renderSearch();

    const input = screen.getByPlaceholderText(
      "Type a command or search...",
    ) as HTMLInputElement;
    await user.type(input, "new");

    // "new" surfaces New Issue + New Project, so cmdk has a multi-item list.
    await waitFor(() => {
      expect(
        screen.getByText((_, el) => el?.textContent === "New Issue" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "New Project" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });

    const selectedValue = () =>
      document
        .querySelector('[cmdk-item=""][aria-selected="true"]')
        ?.getAttribute("data-value") ?? null;

    // cmdk auto-selects the first item; Arrow keys must still move that selection.
    const first = selectedValue();
    expect(first).not.toBeNull();

    await user.keyboard("{ArrowDown}");
    expect(selectedValue()).not.toBe(first);

    await user.keyboard("{ArrowUp}");
    expect(selectedValue()).toBe(first);

    // Home moves the text caret to the start — it must NOT jump the result list.
    await user.keyboard("{Home}");
    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe(0);
    expect(selectedValue()).toBe(first);

    // End moves the caret to the end — again leaving the result selection alone.
    await user.keyboard("{End}");
    expect(input.selectionStart).toBe(input.value.length);
    expect(input.selectionEnd).toBe(input.value.length);
    expect(selectedValue()).toBe(first);
  });

  // MUL-5824: the two searches are ranked independently server-side and the
  // palette renders the whole Projects group before the whole Issues group, so
  // per-type ranking let one cancelled project be the very first row. The
  // partition has to be cross-type and applied here, where results aggregate.
  describe("mixed issue/project cancelled demotion", () => {
    const fixtureIssue = (
      over: Partial<Record<string, unknown>> & { id: string },
    ) => ({
      workspace_id: "ws-test",
      number: 1,
      identifier: "MUL-1",
      title: "Untitled",
      description: null,
      status: "todo",
      priority: "none",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 0,
      start_date: null,
      due_date: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      match_source: "title",
      ...over,
    });

    const fixtureProject = (
      over: Partial<Record<string, unknown>> & { id: string },
    ) => ({
      workspace_id: "ws-test",
      title: "Untitled",
      description: null,
      icon: null,
      status: "in_progress",
      priority: "none",
      lead_type: null,
      lead_id: null,
      start_date: null,
      due_date: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      issue_count: 0,
      match_source: "title",
      ...over,
    });

    /** Rendered result rows, top to bottom, by their cmdk value. */
    const renderedValues = () =>
      Array.from(
        document.querySelectorAll<HTMLElement>('[cmdk-item=""]'),
      )
        .map((el) => el.getAttribute("data-value") ?? "")
        .filter((v) => v.startsWith("project:") || v.startsWith("issue-"));

    /**
     * Group headings, top to bottom. Queried structurally rather than by text:
     * a cancelled project also renders "Cancelled" as its status label.
     */
    const renderedHeadings = () =>
      Array.from(
        document.querySelectorAll<HTMLElement>("[cmdk-group-heading]"),
      ).map((el) => el.textContent ?? "");

    it("renders project status in the selected UI language", async () => {
      const user = userEvent.setup();
      mockSearchProjects.mockResolvedValue({
        projects: [
          fixtureProject({
            id: "proj-localized",
            title: "localized project",
            status: "in_progress",
          }),
        ],
        total: 1,
      });

      renderSearchInChinese();
      await user.type(
        screen.getByPlaceholderText("Type a command or search..."),
        "localized",
      );

      await waitFor(() => expect(screen.getByText("进行中")).toBeInTheDocument(), {
        timeout: 2000,
      });
      expect(screen.queryByText("In Progress")).toBeNull();
    });

    it("keeps a cancelled project below a live issue instead of first", async () => {
      const user = userEvent.setup();
      mockSearchIssues.mockResolvedValue({
        issues: [
          fixtureIssue({
            id: "issue-live",
            number: 10,
            identifier: "MUL-10",
            title: "search live issue",
            status: "in_progress",
          }),
        ],
        total: 1,
      });
      mockSearchProjects.mockResolvedValue({
        projects: [
          fixtureProject({
            id: "proj-dead",
            title: "search dead project",
            status: "cancelled",
          }),
        ],
        total: 1,
      });

      renderSearch();
      await user.type(
        screen.getByPlaceholderText("Type a command or search..."),
        "search",
      );

      await waitFor(
        () => {
          expect(renderedValues()).toEqual(["issue-live", "project:proj-dead"]);
        },
        { timeout: 2000 },
      );
      // Still discoverable, just under its own heading at the bottom.
      expect(renderedHeadings()).toEqual(["Issues", "Cancelled"]);
    });

    it("keeps live rows of both types above every cancelled row", async () => {
      const user = userEvent.setup();
      mockSearchIssues.mockResolvedValue({
        issues: [
          fixtureIssue({
            id: "issue-dead",
            number: 11,
            identifier: "MUL-11",
            title: "search a",
            status: "cancelled",
          }),
          fixtureIssue({
            id: "issue-live",
            number: 12,
            identifier: "MUL-12",
            title: "search b",
            status: "todo",
          }),
          fixtureIssue({
            id: "issue-done",
            number: 13,
            identifier: "MUL-13",
            title: "search c",
            status: "done",
          }),
        ],
        total: 3,
      });
      mockSearchProjects.mockResolvedValue({
        projects: [
          fixtureProject({ id: "proj-dead", title: "search p1", status: "cancelled" }),
          fixtureProject({ id: "proj-live", title: "search p2", status: "planned" }),
        ],
        total: 2,
      });

      renderSearch();
      await user.type(
        screen.getByPlaceholderText("Type a command or search..."),
        "search",
      );

      await waitFor(
        () => {
          expect(renderedValues()).toEqual([
            "project:proj-live",
            // 'done' is live — only cancelled work is demoted.
            "issue-live",
            "issue-done",
            "project:proj-dead",
            "issue-dead",
          ]);
        },
        { timeout: 2000 },
      );
    });

    it("exempts a cancelled issue the query targets by identifier", async () => {
      const user = userEvent.setup();
      mockSearchIssues.mockResolvedValue({
        issues: [
          fixtureIssue({
            id: "issue-hit",
            number: 7,
            identifier: "MUL-7",
            title: "Direct hit",
            status: "cancelled",
          }),
        ],
        total: 1,
      });
      mockSearchProjects.mockResolvedValue({ projects: [], total: 0 });

      renderSearch();
      await user.type(
        screen.getByPlaceholderText("Type a command or search..."),
        "MUL-7",
      );

      await waitFor(
        () => {
          expect(renderedValues()).toEqual(["issue-hit"]);
        },
        { timeout: 2000 },
      );
      expect(renderedHeadings()).not.toContain("Cancelled");
    });
  });

  // Rows from the previous query stay on screen while the next request is in
  // flight (that is what keeps the list from strobing on every keystroke), so
  // they have to stop being *reachable*. cmdk re-selects the first valid item
  // on every input change, so a live stale row means Enter opens a result the
  // user is no longer searching for.
  describe("stale results while the next query is in flight", () => {
    const alphaIssue = {
      id: "issue-alpha",
      workspace_id: "ws-test",
      number: 100,
      identifier: "MUL-100",
      title: "Alpha result",
      description: null,
      status: "todo",
      priority: "none",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "user-1",
      parent_issue_id: null,
      project_id: null,
      position: 0,
      start_date: null,
      due_date: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      match_source: "title",
    };

    /** HighlightText splits the title into <mark> + text, so match the span. */
    const alphaRowTitle = () =>
      screen.getByText(
        (_, el) => el?.textContent === "Alpha result" && el?.tagName === "SPAN",
      );

    /** Resolve the first query; hang on everything after it. */
    const answerOnlyAlpha = () => {
      mockSearchIssues.mockImplementation(({ q }: { q: string }) =>
        q === "alpha"
          ? Promise.resolve({ issues: [alphaIssue], total: 1 })
          : new Promise(() => {}),
      );
      mockSearchProjects.mockImplementation(({ q }: { q: string }) =>
        q === "alpha"
          ? Promise.resolve({ projects: [], total: 0 })
          : new Promise(() => {}),
      );
    };

    const typeAlphaThenChange = async (
      user: ReturnType<typeof userEvent.setup>,
    ) => {
      answerOnlyAlpha();
      renderSearch();
      const input = screen.getByPlaceholderText("Type a command or search...");
      await user.type(input, "alpha");
      await waitFor(
        () => {
          expect(alphaRowTitle()).toBeInTheDocument();
        },
        { timeout: 2000 },
      );
      // Query changes; the response for it never arrives.
      await user.type(input, "zzz");
      return input;
    };

    it("does not open the previous query's result on Enter", async () => {
      const user = userEvent.setup();
      const input = await typeAlphaThenChange(user);

      fireEvent.keyDown(input, { key: "Enter" });

      expect(mockPush).not.toHaveBeenCalled();
      expect(useSearchStore.getState().open).toBe(true);
    });

    it("does not open the previous query's result on click", async () => {
      const user = userEvent.setup();
      await typeAlphaThenChange(user);

      // Still painted (that is the point), but inert.
      fireEvent.click(alphaRowTitle());

      expect(mockPush).not.toHaveBeenCalled();
      expect(useSearchStore.getState().open).toBe(true);
    });
  });
});
