import { act } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SearchCommand } from "./search-command";
import { useSearchStore } from "./search-store";

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
  mockWorkspaces,
  mockCurrentWorkspace,
  mockOpenModal,
  mockToastSuccess,
  mockClipboardWrite,
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
  mockWorkspaces: {
    current: [] as Array<{ id: string; name: string; slug: string }>,
  },
  mockCurrentWorkspace: {
    current: null as { id: string; name: string; slug: string } | null,
  },
  mockOpenModal: vi.fn(),
  mockToastSuccess: vi.fn(),
  mockClipboardWrite: vi.fn(() => Promise.resolve()),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    searchIssues: mockSearchIssues,
    searchProjects: mockSearchProjects,
  },
}));

vi.mock("@multica/core/issues/stores", () => ({
  useRecentIssuesStore: (selector?: (state: { items: typeof mockRecentItems.current }) => unknown) => {
    const state = { items: mockRecentItems.current };
    return selector ? selector(state) : state;
  },
}));

vi.mock("@multica/core", () => ({
  useWorkspaceId: () => "ws-test",
}));

vi.mock("@multica/core/paths", () => ({
  paths: {
    workspace: (slug: string) => ({
      issues: () => `/${slug}/issues`,
    }),
  },
  useCurrentWorkspace: () => mockCurrentWorkspace.current,
  useWorkspacePaths: () => ({
    inbox: () => "/ws-test/inbox",
    myIssues: () => "/ws-test/my-issues",
    issues: () => "/ws-test/issues",
    projects: () => "/ws-test/projects",
    agents: () => "/ws-test/agents",
    runtimes: () => "/ws-test/runtimes",
    skills: () => "/ws-test/skills",
    settings: () => "/ws-test/settings",
    issueDetail: (id: string) => `/ws-test/issues/${id}`,
    projectDetail: (id: string) => `/ws-test/projects/${id}`,
  }),
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_wsId: string, id: string) => ({
    queryKey: ["issues", "ws-test", "detail", id],
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  workspaceListOptions: () => ({ queryKey: ["workspaces", "list"], enabled: false }),
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
    if (key[0] === "workspaces") return { data: mockWorkspaces.current };
    if (opts.enabled === false) return { data: undefined };
    return { data: resolveIssue(key) };
  },
  useQueries: (opts: { queries: Array<{ queryKey: readonly unknown[] }> }) =>
    opts.queries.map((q) => ({ data: resolveIssue(q.queryKey) })),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({
    push: mockPush,
    pathname: mockPathname.current,
    getShareableUrl: mockGetShareableUrl,
  }),
}));

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
    mockSetTheme.mockReset();
    mockTheme.current = "system";
    mockPathname.current = "/ws-test/issues";
    mockGetShareableUrl.mockReset().mockImplementation((p: string) => `https://app.multica/${p}`);
    mockWorkspaces.current = [];
    mockCurrentWorkspace.current = null;
    mockOpenModal.mockReset();
    mockToastSuccess.mockReset();
    mockClipboardWrite.mockReset().mockResolvedValue(undefined);

    // cmdk calls scrollIntoView on the first selected item, which jsdom doesn't implement
    Element.prototype.scrollIntoView = vi.fn();

    act(() => {
      useSearchStore.setState({ open: true });
    });
  });

  it("closes on a single Escape press from the search input", async () => {
    const user = userEvent.setup();

    render(<SearchCommand />);

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.click(input);

    expect(useSearchStore.getState().open).toBe(true);

    await user.keyboard("{Escape}");

    await waitFor(() => {
      expect(useSearchStore.getState().open).toBe(false);
    });
    expect(screen.queryByPlaceholderText("Type a command or search...")).not.toBeInTheDocument();
  });

  it("shows only New Issue by default and hides Pages / Switch Workspace / low-frequency commands until query", () => {
    render(<SearchCommand />);

    expect(screen.queryByText("Pages")).not.toBeInTheDocument();
    expect(screen.queryByText("Switch Workspace")).not.toBeInTheDocument();
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
    render(<SearchCommand />);

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "set");

    await waitFor(() => {
      // HighlightText splits text, so use a function matcher
      expect(screen.getByText((_, el) => el?.textContent === "Settings" && el?.tagName === "SPAN")).toBeInTheDocument();
    });
    expect(screen.queryByText("Inbox")).not.toBeInTheDocument();
  });

  it("navigates to page on selection", async () => {
    const user = userEvent.setup();
    render(<SearchCommand />);

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "settings");

    const settingsItem = await screen.findByText("Settings");
    await user.click(settingsItem);

    expect(mockPush).toHaveBeenCalledWith("/ws-test/settings");
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

    render(<SearchCommand />);

    expect(screen.getByText("Recent")).toBeInTheDocument();
    expect(screen.getByText("First issue")).toBeInTheDocument();
    expect(screen.getByText("MUL-1")).toBeInTheDocument();
    expect(screen.getByText("Second issue")).toBeInTheDocument();
    expect(screen.getByText("MUL-2")).toBeInTheDocument();
  });

  it("shows New Issue / New Project under Commands and triggers the modal store", async () => {
    const user = userEvent.setup();
    render(<SearchCommand />);

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

    expect(mockOpenModal).toHaveBeenCalledWith("create-issue");
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("hides copy-link commands when not on an issue detail route", async () => {
    const user = userEvent.setup();
    mockPathname.current = "/ws-test/projects";
    render(<SearchCommand />);

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
    render(<SearchCommand />);

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

  it("filters theme commands by query keywords", async () => {
    const user = userEvent.setup();
    render(<SearchCommand />);

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
    render(<SearchCommand />);

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
    render(<SearchCommand />);

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

  it("lists other workspaces under Switch Workspace and navigates on select", async () => {
    const user = userEvent.setup();
    mockCurrentWorkspace.current = { id: "ws-current", name: "Current", slug: "current" };
    mockWorkspaces.current = [
      { id: "ws-current", name: "Current", slug: "current" },
      { id: "ws-alpha", name: "Alpha Co", slug: "alpha" },
      { id: "ws-beta", name: "Beta Co", slug: "beta" },
    ];
    render(<SearchCommand />);

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "alpha");

    await waitFor(() => {
      expect(screen.getByText("Switch Workspace")).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Alpha Co" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });
    expect(screen.queryByText("Beta Co")).not.toBeInTheDocument();
    expect(screen.queryByText("Current")).not.toBeInTheDocument();

    const alphaItem = await screen.findByText(
      (_, el) => el?.textContent === "Alpha Co" && el?.tagName === "SPAN",
    );
    await user.click(alphaItem);

    expect(mockPush).toHaveBeenCalledWith("/alpha/issues");
    expect(useSearchStore.getState().open).toBe(false);
  });

  it("shows all other workspaces when typing 'workspace'", async () => {
    const user = userEvent.setup();
    mockCurrentWorkspace.current = { id: "ws-current", name: "Current", slug: "current" };
    mockWorkspaces.current = [
      { id: "ws-current", name: "Current", slug: "current" },
      { id: "ws-alpha", name: "Alpha Co", slug: "alpha" },
      { id: "ws-beta", name: "Beta Co", slug: "beta" },
    ];
    render(<SearchCommand />);

    const input = screen.getByPlaceholderText("Type a command or search...");
    await user.type(input, "workspace");

    await waitFor(() => {
      expect(screen.getByText("Switch Workspace")).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Alpha Co" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
      expect(
        screen.getByText((_, el) => el?.textContent === "Beta Co" && el?.tagName === "SPAN"),
      ).toBeInTheDocument();
    });
    expect(screen.queryByText("Current")).not.toBeInTheDocument();
  });

  it("filters out recent items not present in query cache", () => {
    mockRecentItems.current = [
      { id: "issue-1", visitedAt: 1000 },
      { id: "deleted-issue", visitedAt: 900 },
    ];
    mockAllIssues.current = [
      { id: "issue-1", identifier: "MUL-1", title: "Existing issue", status: "in_progress" },
    ];

    render(<SearchCommand />);

    expect(screen.getByText("Recent")).toBeInTheDocument();
    expect(screen.getByText("Existing issue")).toBeInTheDocument();
    expect(screen.queryByText("deleted-issue")).not.toBeInTheDocument();
  });
});
