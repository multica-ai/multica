import { act, type ReactNode } from "react";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses/queries";
import { useCommandBarContextStore, type CommandBarContext } from "@multica/core/command-bar";
import { inboxUnreadSummaryOptions } from "@multica/core/inbox/queries";
import { I18nProvider } from "@multica/core/i18n/react";
import { issueDetailOptions, issueTimelineOptions } from "@multica/core/issues/queries";
import { WORKSPACE_PAGES } from "@multica/core/paths";
import type { Issue } from "@multica/core/types";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
  workspaceListOptions,
} from "@multica/core/workspace/queries";
import { RESOURCES } from "../locales";
import enLayout from "../locales/en/layout.json";
import zhHansProjects from "../locales/zh-Hans/projects.json";
import { SearchCommand } from "./search-command";
import { useSearchStore } from "./search-store";

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog([]),
}));

const {
  queryData,
  apiFns,
  mockApi,
  mockPush,
  mockPathname,
  mockGetShareableUrl,
  mockRecentItems,
  mockOpenModal,
  mockToast,
  mockSetTheme,
  mockTheme,
  mockCommentCollapseAll,
  mockCommentExpandAll,
  mockResolvedCollapseAll,
  mockResolvedExpandAll,
} = vi.hoisted(() => {
  const apiFns = new Map<string, ReturnType<typeof vi.fn>>();
  const toast = Object.assign(vi.fn(), {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
    message: vi.fn(),
  });
  return {
    queryData: new Map<string, unknown>(),
    apiFns,
    // Any API method resolves to an empty object unless a test sets it.
    mockApi: new Proxy(
      {},
      {
        get(_, prop) {
          if (typeof prop !== "string" || prop === "then") return undefined;
          if (!apiFns.has(prop)) apiFns.set(prop, vi.fn(() => Promise.resolve({})));
          return apiFns.get(prop);
        },
      },
    ),
    mockPush: vi.fn(),
    mockPathname: { current: "/ws-test/issues" },
    mockGetShareableUrl: vi.fn((p: string) => `https://app.multica/${p}`),
    mockRecentItems: { current: [] as Array<{ id: string; visitedAt: number }> },
    mockOpenModal: vi.fn(),
    mockToast: toast,
    mockSetTheme: vi.fn(),
    mockTheme: { current: "system" as "light" | "dark" | "system" },
    mockCommentCollapseAll: vi.fn(),
    mockCommentExpandAll: vi.fn(),
    mockResolvedCollapseAll: vi.fn(),
    mockResolvedExpandAll: vi.fn(),
  };
});

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  type Opts = {
    queryKey: readonly unknown[];
    enabled?: boolean;
    select?: (data: unknown) => unknown;
  };
  const read = (opts: Opts) => {
    if (opts.enabled === false) return undefined;
    const data = queryData.get(JSON.stringify(opts.queryKey));
    return data !== undefined && opts.select ? opts.select(data) : data;
  };
  const fetchLike = (opts: Opts) => {
    const data = queryData.get(JSON.stringify(opts.queryKey));
    return data !== undefined
      ? Promise.resolve(data)
      : Promise.reject(new Error(`unexpected key ${JSON.stringify(opts.queryKey)}`));
  };
  const client = {
    ensureQueryData: fetchLike,
    fetchQuery: fetchLike,
    getQueryData: (key: readonly unknown[]) => queryData.get(JSON.stringify(key)),
    getQueriesData: () => [],
    setQueryData: vi.fn(),
    setQueriesData: vi.fn(),
    invalidateQueries: vi.fn(() => Promise.resolve()),
    cancelQueries: vi.fn(() => Promise.resolve()),
    removeQueries: vi.fn(),
  };
  return {
    ...actual,
    useQuery: (opts: Opts) => {
      const data = read(opts);
      return {
        data,
        isLoading: false,
        isPending: data === undefined,
        isError: false,
        isSuccess: data !== undefined,
      };
    },
    useQueries: (opts: { queries: Opts[]; combine?: (r: unknown[]) => unknown }) => {
      const results = opts.queries.map((q) => ({ data: read(q) }));
      return opts.combine ? opts.combine(results) : results;
    },
    useQueryClient: () => client,
    // Mutations run their mutationFn (which calls the mocked api) and the
    // per-call callbacks; cache side effects are out of scope here.
    useMutation: (opts: { mutationFn?: (vars: unknown) => Promise<unknown> }) => {
      const run = (vars: unknown) => Promise.resolve().then(() => opts.mutationFn?.(vars));
      return {
        mutateAsync: run,
        mutate: (
          vars: unknown,
          cbs?: { onSuccess?: (d: unknown) => void; onError?: (e: unknown) => void },
        ) => {
          run(vars).then(
            (d) => cbs?.onSuccess?.(d),
            (e) => cbs?.onError?.(e),
          );
        },
        isPending: false,
        reset: () => {},
      };
    },
  };
});

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: mockApi,
}));

vi.mock("@multica/core/auth", async (importOriginal) => {
  const state = { user: { id: "user-me", name: "Me Myself", email: "me@example.com" } };
  return {
    ...(await importOriginal<typeof import("@multica/core/auth")>()),
    useAuthStore: Object.assign((selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state), {
      getState: () => state,
    }),
  };
});

vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core")>()),
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("ws-test"),
    useCurrentWorkspace: () => ({ id: "ws-test", slug: "ws-test", name: "Test Workspace" }),
  };
});

vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: ({ actorType, actorId }: { actorType: string; actorId: string }) => (
    <span data-testid="actor-avatar" title={`${actorType}:${actorId}`} />
  ),
}));

vi.mock("@multica/core/issues/stores", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/issues/stores")>();
  const EMPTY: Array<{ id: string; visitedAt: number }> = [];
  return {
    ...actual,
    useRecentIssuesStore: (
      selector?: (state: { byWorkspace: Record<string, typeof mockRecentItems.current> }) => unknown,
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
      getState: () => ({ collapseAll: mockCommentCollapseAll, expandAll: mockCommentExpandAll }),
    }),
    useResolvedExpandStore: Object.assign(vi.fn(), {
      getState: () => ({ collapseAll: mockResolvedCollapseAll, expandAll: mockResolvedExpandAll }),
    }),
  };
});

vi.mock("@multica/core/modals", async (importOriginal) => {
  const state = { open: mockOpenModal };
  return {
    ...(await importOriginal<typeof import("@multica/core/modals")>()),
    useModalStore: Object.assign((selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state), {
      getState: () => state,
    }),
  };
});

// Mock the context module, not the barrel: resolveClickIntent and
// useIntentNavigate stay the REAL implementations and read this adapter
// (no openInNewTab — the web shape, so tab intents go through window.open).
vi.mock("../navigation/context", () => {
  const adapter = () => ({
    push: mockPush,
    pathname: mockPathname.current,
    getShareableUrl: mockGetShareableUrl,
  });
  return { useNavigation: adapter, useOptionalNavigation: adapter };
});

vi.mock("@multica/ui/components/common/theme-provider", () => ({
  useTheme: () => ({ theme: mockTheme.current, setTheme: mockSetTheme }),
}));

vi.mock("sonner", () => ({ toast: mockToast }));

const PLACEHOLDER = "Search, or type what needs doing...";
const ME = "user-me";

function I18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={{ en: RESOURCES.en }}>
      {children}
    </I18nProvider>
  );
}

// Deliberately NOT a full zh-Hans bundle: only `projects` is translated, so
// the chrome stays addressable by its English strings while a project row's
// status has to come through the projects namespace.
function ChineseI18nWrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider
      locale="zh-Hans"
      resources={{ "zh-Hans": { ...RESOURCES.en, projects: zhHansProjects } }}
    >
      {children}
    </I18nProvider>
  );
}

const renderSearch = () => render(<SearchCommand />, { wrapper: I18nWrapper });
const renderSearchInChinese = () => render(<SearchCommand />, { wrapper: ChineseI18nWrapper });
const input = () => screen.getByPlaceholderText(PLACEHOLDER) as HTMLInputElement;

function seed(options: { queryKey: readonly unknown[] }, data: unknown) {
  queryData.set(JSON.stringify(options.queryKey), data);
}

function api(name: string): Mock {
  return (mockApi as Record<string, Mock>)[name]!;
}

function makeIssue(over: Partial<Issue> & { id: string }): Issue {
  return {
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
    creator_id: ME,
    parent_issue_id: null,
    project_id: null,
    position: 0,
    start_date: null,
    due_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  } as Issue;
}

function makeAgent(id: string, name: string, over: Record<string, unknown> = {}) {
  return {
    id,
    workspace_id: "ws-test",
    name,
    description: "",
    avatar_url: null,
    owner_id: ME,
    permission_mode: "private",
    runtime_id: "rt-1",
    runtime_bound: true,
    archived_at: null,
    ...over,
  };
}

function makeMember(userId: string, name: string, email: string) {
  return {
    id: `member-${userId}`,
    workspace_id: "ws-test",
    user_id: userId,
    role: "member" as const,
    created_at: "2026-01-01T00:00:00Z",
    name,
    email,
    avatar_url: null,
  };
}

const registered: string[] = [];
function registerContext(context: CommandBarContext) {
  const token = `test-${registered.length}`;
  registered.push(token);
  act(() => useCommandBarContextStore.getState().register(token, context));
}

/** Rendered cmdk rows, top to bottom, by their value. */
const renderedValues = () =>
  Array.from(document.querySelectorAll<HTMLElement>('[cmdk-item=""]')).map(
    (el) => el.getAttribute("data-value") ?? "",
  );

/** Group headings, top to bottom. */
const renderedHeadings = () =>
  Array.from(document.querySelectorAll<HTMLElement>("[cmdk-group-heading]")).map(
    (el) => el.textContent ?? "",
  );

const selectedValue = () =>
  document.querySelector('[cmdk-item=""][aria-selected="true"]')?.getAttribute("data-value") ?? null;

const rowText = (text: string) =>
  screen.findByText((_, el) => el?.textContent === text && el?.tagName === "SPAN");

describe("SearchCommand", () => {
  beforeEach(() => {
    queryData.clear();
    apiFns.clear();
    apiFns.set("getBaseUrl", vi.fn(() => "http://127.0.0.1:8080"));
    apiFns.set("searchIssues", vi.fn(() => Promise.resolve({ issues: [], total: 0 })));
    apiFns.set("searchProjects", vi.fn(() => Promise.resolve({ projects: [], total: 0 })));
    mockPush.mockReset();
    mockPathname.current = "/ws-test/issues";
    mockGetShareableUrl.mockReset().mockImplementation((p: string) => `https://app.multica/${p}`);
    mockRecentItems.current = [];
    mockOpenModal.mockReset();
    mockToast.mockReset();
    mockToast.success.mockReset();
    mockToast.error.mockReset();
    mockToast.warning.mockReset();
    mockSetTheme.mockReset();
    mockTheme.current = "system";
    mockCommentCollapseAll.mockReset();
    mockCommentExpandAll.mockReset();
    mockResolvedCollapseAll.mockReset();
    mockResolvedExpandAll.mockReset();

    seed(memberListOptions("ws-test"), [makeMember(ME, "Me Myself", "me@example.com")]);
    seed(agentListOptions("ws-test"), []);
    seed(squadListOptions("ws-test"), []);

    act(() => {
      for (const token of registered.splice(0)) useCommandBarContextStore.getState().unregister(token);
      useSearchStore.setState({ open: true });
    });

    // cmdk calls scrollIntoView on the first selected item, which jsdom doesn't implement
    Element.prototype.scrollIntoView = vi.fn();
  });

  it("closes on a single Escape press from the search input", async () => {
    const user = userEvent.setup();
    renderSearch();
    await user.click(input());
    await user.keyboard("{Escape}");

    await waitFor(() => expect(useSearchStore.getState().open).toBe(false));
    expect(screen.queryByPlaceholderText(PLACEHOLDER)).not.toBeInTheDocument();
  });

  it("shows only creation shortcuts on an empty query off any object page", () => {
    renderSearch();

    expect(screen.queryByText("Pages")).not.toBeInTheDocument();
    expect(renderedHeadings()).toEqual(["New"]);
    expect(renderedValues()).toEqual(["command:new-issue", "command:chat-with-agent"]);
  });

  describe("pages and global commands", () => {
    it("filters navigation pages by query", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "set");

      expect(await rowText("Settings")).toBeInTheDocument();
      expect(screen.queryByText("Inbox")).not.toBeInTheDocument();
    });

    it("offers every workspace nav page, not a hand-maintained subset", async () => {
      const user = userEvent.setup();
      renderSearch();
      // The Pages group is generated from WORKSPACE_PAGES — the registry the
      // sidebar and the desktop tab bar read (MUL-6272).
      for (const [key, page] of Object.entries(WORKSPACE_PAGES)) {
        await user.clear(input());
        await user.type(input(), enLayout.nav[page.navKey]);
        await waitFor(() => expect(renderedValues()).toContain(`page:${key}`));
      }
    });

    it("does not surface a page on an incidental substring of a hidden keyword", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "s");

      expect(await rowText("Settings")).toBeInTheDocument();
      expect(screen.queryByText("Inbox")).not.toBeInTheDocument();
    });

    it("navigates to a page whose label differs from its route segment", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "analytics");
      await user.click(await screen.findByText("Analytics"));

      expect(mockPush).toHaveBeenCalledWith("/ws-test/usage");
      expect(useSearchStore.getState().open).toBe(false);
    });

    it("cmd-click on a result opens it in a new tab instead of navigating in place", async () => {
      const user = userEvent.setup();
      const open = vi.spyOn(window, "open").mockReturnValue(null);
      renderSearch();
      await user.type(input(), "settings");
      fireEvent.click(await screen.findByText("Settings"), { metaKey: true });

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
      await user.type(input(), "settings");
      await screen.findByText("Settings");
      fireEvent.keyDown(input(), { key: "Enter", metaKey: true });

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
      fireEvent.click(input(), { metaKey: true });
      await user.type(input(), "settings");
      await user.click(await screen.findByText("Settings"));

      expect(mockPush).toHaveBeenCalledWith("/ws-test/settings");
      expect(open).not.toHaveBeenCalled();
      open.mockRestore();
    });

    it("opens New issue and New project through the modal store", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "new");

      expect(await rowText("New project")).toBeInTheDocument();
      await user.click(await rowText("New issue"));

      expect(mockOpenModal).toHaveBeenCalledWith("quick-create-issue", null);
      expect(useSearchStore.getState().open).toBe(false);
    });

    it("changes theme through a submenu that marks the current theme", async () => {
      const user = userEvent.setup();
      mockTheme.current = "dark";
      renderSearch();
      await user.type(input(), "theme");
      await user.click(await rowText("Change theme..."));

      expect(within(screen.getByRole("navigation", { name: "Current menu" })).getByText("Change theme")).toBeInTheDocument();
      expect(renderedValues()).toEqual(["theme:light", "theme:dark", "theme:system"]);
      expect(screen.getByLabelText("Current theme")).toBeInTheDocument();

      await user.click(screen.getByText("Light"));
      expect(mockSetTheme).toHaveBeenCalledWith("light");
      expect(useSearchStore.getState().open).toBe(false);
    });

    it("Backspace on an empty submenu input returns to the root", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "theme");
      await user.click(await rowText("Change theme..."));

      const pageInput = screen.getByPlaceholderText("Filter...");
      await user.type(pageInput, "da");
      expect(renderedValues()).toEqual(["theme:dark"]);
      await user.keyboard("{Backspace}{Backspace}");
      // Clearing the filter keeps the submenu; one more Backspace leaves it.
      expect(screen.getByPlaceholderText("Filter...")).toBeInTheDocument();
      await user.keyboard("{Backspace}");

      expect(await screen.findByPlaceholderText(PLACEHOLDER)).toBeInTheDocument();
      expect(screen.queryByRole("navigation", { name: "Current menu" })).not.toBeInTheDocument();
    });

    it("lets Home/End edit the query caret instead of hijacking result navigation", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "new");
      await rowText("New project");

      const first = selectedValue();
      expect(first).not.toBeNull();
      await user.keyboard("{ArrowDown}");
      expect(selectedValue()).not.toBe(first);
      await user.keyboard("{ArrowUp}");
      expect(selectedValue()).toBe(first);

      await user.keyboard("{Home}");
      expect(input().selectionStart).toBe(0);
      expect(selectedValue()).toBe(first);
      await user.keyboard("{End}");
      expect(input().selectionStart).toBe(input().value.length);
      expect(selectedValue()).toBe(first);
    });
  });

  describe("scope prefixes", () => {
    it("'>' limits the list to commands", async () => {
      const user = userEvent.setup();
      seed(agentListOptions("ws-test"), [makeAgent("agent-newton", "Newton")]);
      renderSearch();
      await user.type(input(), ">new");

      await rowText("New project");
      expect(renderedValues().every((v) => v.startsWith("command:"))).toBe(true);
      expect(api("searchIssues")).not.toHaveBeenCalled();
    });

    it("'@' limits the list to people and agents", async () => {
      const user = userEvent.setup();
      seed(agentListOptions("ws-test"), [makeAgent("agent-lambda", "Lambda")]);
      renderSearch();
      await user.type(input(), "@lam");

      await waitFor(() => expect(renderedValues()).toEqual(["agent:agent-lambda"]));
      expect(api("searchIssues")).not.toHaveBeenCalled();
    });

    it("'#' searches issues only", async () => {
      const user = userEvent.setup();
      api("searchIssues").mockResolvedValue({
        issues: [makeIssue({ id: "issue-settings", identifier: "MUL-5", title: "settings crash" })],
        total: 1,
      });
      renderSearch();
      await user.type(input(), "#settings");

      await waitFor(() => expect(renderedValues()).toEqual(["issue-settings"]), { timeout: 2000 });
      expect(api("searchProjects")).not.toHaveBeenCalled();
    });
  });

  describe("search results", () => {
    it("ranks an agent typed by name as the best match with its actions", async () => {
      const user = userEvent.setup();
      seed(agentListOptions("ws-test"), [
        makeAgent("agent-alpha", "Alpha Lambda Helper"),
        makeAgent("agent-lambda", "Lambda"),
      ]);
      renderSearch();
      await user.type(input(), "lambda");

      await waitFor(() => expect(renderedValues()[0]).toBe("best:agent:agent-lambda"));
      expect(renderedHeadings().slice(0, 2)).toEqual(["Best match", "Actions"]);
      expect(renderedValues()).toContain("best-action:chat");
    });

    it("lists workspace members and navigates to the member page", async () => {
      const user = userEvent.setup();
      seed(memberListOptions("ws-test"), [
        makeMember(ME, "Me Myself", "me@example.com"),
        makeMember("user-1", "Alice Zhang", "alice@example.com"),
        makeMember("user-2", "Bob Liu", "bob@example.com"),
      ]);
      renderSearch();
      await user.type(input(), "alice");

      await waitFor(() => expect(renderedValues()[0]).toBe("best:member:user-1"));
      expect(screen.queryByText("Bob Liu")).not.toBeInTheDocument();
      fireEvent.keyDown(input(), { key: "Enter" });

      expect(mockPush).toHaveBeenCalledWith("/ws-test/members/user-1");
    });

    it("renders recent issues joined from the query cache with type labels", () => {
      mockRecentItems.current = [
        { id: "issue-1", visitedAt: 1000 },
        { id: "deleted-issue", visitedAt: 950 },
        { id: "issue-2", visitedAt: 900 },
      ];
      seed(issueDetailOptions("ws-test", "issue-1"), makeIssue({ id: "issue-1", identifier: "MUL-1", title: "First issue" }));
      seed(
        issueDetailOptions("ws-test", "issue-2"),
        makeIssue({ id: "issue-2", identifier: "MUL-2", title: "Second issue", status: "done" }),
      );
      renderSearch();

      expect(screen.getByText("Recent")).toBeInTheDocument();
      expect(screen.getByText("First issue")).toBeInTheDocument();
      expect(screen.getByText("Second issue")).toBeInTheDocument();
      expect(screen.getAllByText("Issue")).toHaveLength(2);
      expect(screen.queryByText("deleted-issue")).not.toBeInTheDocument();
    });

    it("renders description and comment snippets regardless of match_source", async () => {
      const user = userEvent.setup();
      api("searchIssues").mockResolvedValue({
        issues: [
          {
            ...makeIssue({ id: "issue-snippet", identifier: "MUL-99", title: "HTML rendering pipeline" }),
            match_source: "title",
            matched_description_snippet: "...uses HTML templates for rendering...",
            matched_comment_snippet: "...we should migrate away from HTML...",
          },
        ],
        total: 1,
      });
      renderSearch();
      await user.type(input(), "html");

      await waitFor(() => expect(renderedValues()).toContain("issue-snippet"), { timeout: 2000 });
      expect(
        screen.getByText((_, el) => !!el?.textContent?.includes("uses HTML templates") && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => !!el?.textContent?.includes("migrate away from HTML") && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });
  });

  describe("workspace switching", () => {
    beforeEach(() => {
      seed(workspaceListOptions(), [
        { id: "ws-zeta", name: "Zeta Labs", slug: "zeta", avatar_url: null, created_at: "2025-01-01T00:00:00Z" },
        { id: "ws-test", name: "Test Workspace", slug: "ws-test", avatar_url: null, created_at: "2025-06-01T00:00:00Z" },
        { id: "ws-acme", name: "Acme Corp", slug: "acme-hq", avatar_url: null, created_at: "2026-01-01T00:00:00Z" },
      ]);
      seed(inboxUnreadSummaryOptions(), [
        { workspace_id: "ws-acme", count: 2 },
        { workspace_id: "ws-test", count: 5 },
      ]);
    });

    const workspaceRows = () =>
      within(screen.getByRole("group", { name: "Workspaces" })).getAllByRole("option");

    it("hides the Workspaces group on an empty query", () => {
      renderSearch();
      expect(screen.queryByText("Workspaces")).not.toBeInTheDocument();
    });

    it('lists every other workspace for "workspace", oldest first, with unread dots', async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "workspace");

      await waitFor(() =>
        expect(workspaceRows().map((row) => row.textContent)).toEqual([
          expect.stringContaining("Zeta Labs"),
          expect.stringContaining("Acme Corp"),
        ]),
      );
      const [zeta, acme] = workspaceRows();
      expect(within(zeta!).queryByTestId("workspace-unread-dot")).toBeNull();
      expect(within(acme!).getByTestId("workspace-unread-dot")).toBeInTheDocument();
    });

    it("switches to a workspace by slug", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "acme");
      fireEvent.click(await rowText("Acme Corp"));

      expect(mockPush).toHaveBeenCalledWith("/acme-hq/issues");
    });
  });

  describe("current issue", () => {
    const issue = makeIssue({ id: "issue-1", identifier: "MUL-42", title: "Safari login loop" });

    beforeEach(() => {
      seed(issueDetailOptions("ws-test", "issue-1"), issue);
      registerContext({ kind: "issue", issue });
    });

    it("leads the empty state with the issue's commands under its own heading", () => {
      renderSearch();

      expect(renderedHeadings()[0]).toContain("Current issue");
      expect(renderedHeadings()[0]).toContain("MUL-42");
      expect(renderedValues().slice(0, 4)).toEqual([
        "command:context-issue:issue-status",
        "command:context-issue:issue-assignee",
        "command:context-issue:issue-priority",
        "command:context-issue:issue-copy-link",
      ]);
      // The input stays a global search: no context chip inside it.
      expect(input().value).toBe("");
    });

    it("opens a submenu from its row, with the issue in the breadcrumb", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.click(await rowText("Change status..."));

      expect(screen.getByRole("navigation", { name: "Current menu" })).toHaveTextContent("MUL-42Change status");
      expect(renderedValues().length).toBeGreaterThan(0);
      expect(renderedValues().every((v) => v.startsWith("status:"))).toBe(true);
    });

    it("ranks the issue's command first when its name is typed", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "status");

      await waitFor(() => expect(renderedValues()[0]).toBe("command:context-issue:issue-status"));
      fireEvent.keyDown(input(), { key: "Enter" });
      expect(screen.getByRole("navigation", { name: "Current menu" })).toHaveTextContent("MUL-42Change status");
    });

    it.each(["s", "a", "p"])("types '%s' into the search instead of opening a submenu", async (key) => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), key);

      expect(input().value).toBe(key);
      expect(screen.queryByRole("navigation", { name: "Current menu" })).not.toBeInTheDocument();
    });

    it("searches a person whose name starts with A instead of assigning", async () => {
      const user = userEvent.setup();
      seed(memberListOptions("ws-test"), [
        makeMember(ME, "Me Myself", "me@example.com"),
        makeMember("user-1", "Alice Zhang", "alice@example.com"),
      ]);
      renderSearch();
      await user.type(input(), "alice");

      await waitFor(() => expect(renderedValues()[0]).toBe("best:member:user-1"));
      fireEvent.keyDown(input(), { key: "Enter" });

      expect(mockPush).toHaveBeenCalledWith("/ws-test/members/user-1");
      expect(api("updateIssue")).not.toHaveBeenCalled();
      expect(mockOpenModal).not.toHaveBeenCalled();
    });

    it("writes a priority picked in the submenu to the issue", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.click(await rowText("Set priority..."));
      await user.click(screen.getByText("High"));

      await waitFor(() => expect(api("updateIssue")).toHaveBeenCalledWith("issue-1", { priority: "high" }));
      expect(useSearchStore.getState().open).toBe(false);
    });

    it("copies the issue link and identifier", async () => {
      const user = userEvent.setup();
      const writeSpy = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
      renderSearch();
      await user.type(input(), "copy");
      await user.click(await rowText("Copy issue link"));

      await waitFor(() => expect(writeSpy).toHaveBeenCalledWith("https://app.multica//ws-test/issues/MUL-42"));

      act(() => useSearchStore.setState({ open: true }));
      await user.type(input(), "copy");
      await user.click(await rowText("Copy identifier (MUL-42)"));
      await waitFor(() => expect(writeSpy).toHaveBeenCalledWith("MUL-42"));
      expect(mockToast.success).toHaveBeenCalledWith("Copied MUL-42");
      writeSpy.mockRestore();
    });

    it("folds all comments, including expanded resolved threads", async () => {
      const user = userEvent.setup();
      seed(issueTimelineOptions("issue-1"), [
        { type: "activity", id: "act-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T00:00:00Z", action: "status_changed" },
        { type: "comment", id: "root-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T01:00:00Z", parent_id: null },
        { type: "comment", id: "reply-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T02:00:00Z", parent_id: "root-1" },
        { type: "comment", id: "root-2", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T03:00:00Z", parent_id: null, resolved_at: "2026-01-02T00:00:00Z" },
      ]);
      renderSearch();
      await user.type(input(), "fold");
      await user.click(await rowText("Fold all comments"));

      await waitFor(() => expect(mockCommentCollapseAll).toHaveBeenCalledWith("issue-1", ["root-1", "root-2"]));
      expect(mockResolvedCollapseAll).toHaveBeenCalledWith("issue-1");
      expect(mockCommentExpandAll).not.toHaveBeenCalled();
    });

    it("unfolds all comments and expands resolved threads", async () => {
      const user = userEvent.setup();
      seed(issueTimelineOptions("issue-1"), [
        { type: "comment", id: "root-1", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T01:00:00Z", parent_id: null },
        { type: "comment", id: "root-2", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T03:00:00Z", parent_id: null, resolved_at: "2026-01-02T00:00:00Z" },
        { type: "comment", id: "root-3", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T04:00:00Z", parent_id: null },
        { type: "comment", id: "reply-3", actor_type: "member", actor_id: "u1", created_at: "2026-01-01T05:00:00Z", parent_id: "root-3", resolved_at: "2026-01-02T01:00:00Z" },
      ]);
      renderSearch();
      await user.type(input(), "unfold");
      await user.click(await rowText("Unfold all comments"));

      await waitFor(() => expect(mockCommentExpandAll).toHaveBeenCalledWith("issue-1"));
      expect(mockResolvedExpandAll).toHaveBeenCalledWith("issue-1", ["root-2", "root-3"]);
      expect(mockCommentCollapseAll).not.toHaveBeenCalled();
    });

    it("offers assigning the open issue to a best-matched agent", async () => {
      const user = userEvent.setup();
      seed(agentListOptions("ws-test"), [makeAgent("agent-lambda", "Lambda")]);
      renderSearch();
      await user.type(input(), "lambda");

      await user.click(await rowText("Assign MUL-42 to Lambda"));
      // Handing a live issue to an agent goes through the run-confirm gate.
      expect(mockOpenModal).toHaveBeenCalledWith(
        "issue-run-confirm",
        expect.objectContaining({ issueIds: ["issue-1"], assigneeType: "agent", assigneeId: "agent-lambda" }),
      );
    });

    it("hides issue commands once the page unregisters", () => {
      act(() => {
        for (const token of registered.splice(0)) useCommandBarContextStore.getState().unregister(token);
      });
      renderSearch();

      expect(renderedValues()).not.toContain("command:context-issue:issue-status");
    });
  });

  describe("selected issues", () => {
    const issues = [
      makeIssue({ id: "issue-a", identifier: "MUL-1", status: "todo" }),
      makeIssue({ id: "issue-b", identifier: "MUL-2", status: "in_progress" }),
      makeIssue({ id: "issue-c", identifier: "MUL-3", status: "todo" }),
    ];

    beforeEach(() => {
      registerContext({ kind: "issues", issues, clearSelection: vi.fn() });
    });

    it("applies a priority to every selected issue in one batch write", async () => {
      const user = userEvent.setup();
      api("batchUpdateIssues").mockResolvedValue({ updated: 3 });
      renderSearch();

      expect(renderedHeadings()[0]).toBe("3 selected issues");
      await user.click(await rowText("Set priority..."));
      expect(screen.getByRole("navigation", { name: "Current menu" })).toHaveTextContent(
        "3 selected issuesSet priority",
      );
      await user.click(screen.getByText("Urgent"));

      await waitFor(() =>
        expect(api("batchUpdateIssues")).toHaveBeenCalledWith(["issue-a", "issue-b", "issue-c"], { priority: "urgent" }),
      );
      expect(api("updateIssue")).not.toHaveBeenCalled();
    });

    it("previews the run fan-out before assigning the selection to an agent", async () => {
      const user = userEvent.setup();
      seed(agentListOptions("ws-test"), [makeAgent("agent-lambda", "Lambda")]);
      renderSearch();
      await user.click(await rowText("Assign to..."));
      await user.click(await screen.findByText((_, el) => el?.textContent === "Lambda" && el?.tagName === "SPAN"));

      expect(mockOpenModal).toHaveBeenCalledWith("issue-run-confirm", {
        issueIds: ["issue-a", "issue-b", "issue-c"],
        mode: "assign",
        assigneeType: "agent",
        assigneeId: "agent-lambda",
      });
      expect(api("batchUpdateIssues")).not.toHaveBeenCalled();
    });
  });

  describe("previewed issue inside a selection", () => {
    const issues = [
      makeIssue({ id: "issue-a", identifier: "MUL-1" }),
      makeIssue({ id: "issue-b", identifier: "MUL-2" }),
      makeIssue({ id: "issue-c", identifier: "MUL-3" }),
    ];

    beforeEach(() => {
      registerContext({ kind: "issues", issues, clearSelection: vi.fn() });
      // The preview opens after the selection, so its group renders first.
      registerContext({ kind: "issue", issue: issues[0]! });
    });

    it("runs the selection's command when its row is highlighted, not the issue's", async () => {
      api("batchUpdateIssues").mockResolvedValue({ updated: 3 });
      renderSearch();

      const values = renderedValues();
      expect(new Set(values).size).toBe(values.length);
      expect(renderedHeadings().slice(0, 2)).toEqual([
        expect.stringContaining("Current issue"),
        "3 selected issues",
      ]);

      const selectionRow = document.querySelector<HTMLElement>(
        '[cmdk-item=""][data-value="command:context-issues:issue-priority"]',
      )!;
      fireEvent.pointerMove(selectionRow);
      expect(selectedValue()).toBe("command:context-issues:issue-priority");
      fireEvent.keyDown(input(), { key: "Enter" });

      expect(screen.getByRole("navigation", { name: "Current menu" })).toHaveTextContent(
        "3 selected issuesSet priority",
      );
      fireEvent.click(screen.getByText("Urgent"));
      await waitFor(() =>
        expect(api("batchUpdateIssues")).toHaveBeenCalledWith(["issue-a", "issue-b", "issue-c"], { priority: "urgent" }),
      );
      expect(api("updateIssue")).not.toHaveBeenCalled();
    });
  });

  describe("hand to agent", () => {
    beforeEach(() => {
      seed(agentListOptions("ws-test"), [
        makeAgent("agent-lambda", "Lambda"),
        makeAgent("agent-sigma", "Sigma"),
      ]);
      api("quickCreateIssue").mockResolvedValue({ task_id: "task-1" });
    });

    it("turns an instruction naming an agent into a quick-create for it", async () => {
      const user = userEvent.setup();
      const prompt = "让 Lambda 修一下 Safari 登录回调的 500";
      renderSearch();
      await user.type(input(), prompt);

      await waitFor(() => expect(renderedValues()[0]).toBe("delegate:submit"));
      expect(await rowText("New issue and assign to Lambda")).toBeInTheDocument();
      fireEvent.keyDown(input(), { key: "Enter" });

      await waitFor(() =>
        expect(api("quickCreateIssue")).toHaveBeenCalledWith({
          agent_id: "agent-lambda",
          prompt,
          project_id: undefined,
          parent_issue_id: undefined,
        }),
      );
      expect(mockToast.success).toHaveBeenCalled();
      expect(useSearchStore.getState().open).toBe(false);
    });

    it("attaches the open issue as parent", async () => {
      const user = userEvent.setup();
      const parent = makeIssue({ id: "issue-9", identifier: "MUL-9" });
      registerContext({ kind: "issue", issue: parent });
      renderSearch();
      await user.type(input(), "ask Sigma to write the release notes");

      await waitFor(() => expect(renderedValues()[0]).toBe("delegate:submit"));
      expect(screen.getByText("Under MUL-9")).toBeInTheDocument();
      fireEvent.keyDown(input(), { key: "Enter" });

      await waitFor(() =>
        expect(api("quickCreateIssue")).toHaveBeenCalledWith(
          expect.objectContaining({ agent_id: "agent-sigma", parent_issue_id: "issue-9" }),
        ),
      );
    });

    it("Shift+Enter falls back to a keyword search", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "fix the Safari login callback 500");
      await waitFor(() => expect(renderedValues()[0]).toBe("delegate:submit"));

      fireEvent.keyDown(input(), { key: "Enter", shiftKey: true });

      await waitFor(() => expect(renderedValues()).not.toContain("delegate:submit"));
      expect(api("quickCreateIssue")).not.toHaveBeenCalled();
    });

    it("does not read a plain keyword as an instruction", async () => {
      const user = userEvent.setup();
      renderSearch();
      await user.type(input(), "login");

      await waitFor(() => expect(api("searchIssues")).toHaveBeenCalled(), { timeout: 2000 });
      expect(renderedValues()).not.toContain("delegate:submit");
    });
  });

  // MUL-5824: the two searches are ranked independently server-side, so the
  // cancelled partition has to be cross-type and applied where results meet.
  describe("mixed issue/project cancelled demotion", () => {
    const fixtureProject = (over: Partial<Record<string, unknown>> & { id: string }) => ({
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
    const fixtureIssue = (over: Partial<Issue> & { id: string }) => ({
      ...makeIssue(over),
      match_source: "title",
    });
    const resultValues = () =>
      renderedValues().filter((v) => v.startsWith("project:") || v.startsWith("issue-"));

    it("renders project status in the selected UI language", async () => {
      const user = userEvent.setup();
      api("searchProjects").mockResolvedValue({
        projects: [fixtureProject({ id: "proj-localized", title: "localized project" })],
        total: 1,
      });
      renderSearchInChinese();
      await user.type(input(), "localized");

      await waitFor(() => expect(screen.getByText("进行中")).toBeInTheDocument(), { timeout: 2000 });
      expect(screen.queryByText("In Progress")).toBeNull();
    });

    it("keeps live rows of both types above every cancelled row", async () => {
      const user = userEvent.setup();
      api("searchIssues").mockResolvedValue({
        issues: [
          fixtureIssue({ id: "issue-dead", number: 11, identifier: "MUL-11", title: "search a", status: "cancelled" }),
          fixtureIssue({ id: "issue-live", number: 12, identifier: "MUL-12", title: "search b", status: "todo" }),
          fixtureIssue({ id: "issue-done", number: 13, identifier: "MUL-13", title: "search c", status: "done" }),
        ],
        total: 3,
      });
      api("searchProjects").mockResolvedValue({
        projects: [
          fixtureProject({ id: "proj-dead", title: "search p1", status: "cancelled" }),
          fixtureProject({ id: "proj-live", title: "search p2", status: "planned" }),
        ],
        total: 2,
      });
      renderSearch();
      await user.type(input(), "search");

      await waitFor(
        () =>
          expect(resultValues()).toEqual([
            "project:proj-live",
            "issue-live",
            "issue-done",
            "project:proj-dead",
            "issue-dead",
          ]),
        { timeout: 2000 },
      );
      expect(renderedHeadings().at(-1)).toBe("Cancelled");
    });

    it("exempts a cancelled issue the query targets by identifier", async () => {
      const user = userEvent.setup();
      api("searchIssues").mockResolvedValue({
        issues: [fixtureIssue({ id: "issue-hit", number: 7, identifier: "MUL-7", title: "Direct hit", status: "cancelled" })],
        total: 1,
      });
      renderSearch();
      await user.type(input(), "MUL-7");

      await waitFor(() => expect(resultValues()).toEqual(["issue-hit"]), { timeout: 2000 });
      expect(renderedHeadings()).not.toContain("Cancelled");
    });
  });

  // Rows from the previous query stay painted while the next request is in
  // flight, so they have to stop being reachable: cmdk re-selects the first
  // valid item on every input change.
  describe("stale results while the next query is in flight", () => {
    const alphaRowTitle = () =>
      screen.getByText((_, el) => el?.textContent === "Alpha result" && el?.tagName === "SPAN");

    const typeAlphaThenChange = async (user: ReturnType<typeof userEvent.setup>) => {
      api("searchIssues").mockImplementation(({ q }: { q: string }) =>
        q === "alpha"
          ? Promise.resolve({
              issues: [{ ...makeIssue({ id: "issue-alpha", identifier: "MUL-100", title: "Alpha result" }), match_source: "title" }],
              total: 1,
            })
          : new Promise(() => {}),
      );
      api("searchProjects").mockImplementation(({ q }: { q: string }) =>
        q === "alpha" ? Promise.resolve({ projects: [], total: 0 }) : new Promise(() => {}),
      );
      renderSearch();
      await user.type(input(), "alpha");
      await waitFor(() => expect(alphaRowTitle()).toBeInTheDocument(), { timeout: 2000 });
      await user.type(input(), "zzz");
    };

    it("does not open the previous query's result on Enter", async () => {
      const user = userEvent.setup();
      await typeAlphaThenChange(user);
      fireEvent.keyDown(input(), { key: "Enter" });

      expect(mockPush).not.toHaveBeenCalled();
      expect(useSearchStore.getState().open).toBe(true);
    });

    it("does not open the previous query's result on click", async () => {
      const user = userEvent.setup();
      await typeAlphaThenChange(user);
      fireEvent.click(alphaRowTitle());

      expect(mockPush).not.toHaveBeenCalled();
      expect(useSearchStore.getState().open).toBe(true);
    });
  });
});
