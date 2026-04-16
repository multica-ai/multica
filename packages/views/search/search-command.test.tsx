import { act } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SearchCommand } from "./search-command";
import { useSearchStore } from "./search-store";

const { mockPush, mockSearchIssues, mockSearchProjects, mockRecentItems, mockAllIssues } = vi.hoisted(() => ({
  mockPush: vi.fn(),
  mockSearchIssues: vi.fn(),
  mockSearchProjects: vi.fn(),
  mockRecentItems: { current: [] as Array<{ id: string; visitedAt: number }> },
  mockAllIssues: { current: [] as Array<Record<string, unknown>> },
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
  issueListOptions: () => ({ queryKey: ["issues", "ws-test", "list"], enabled: false }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: mockAllIssues.current }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({
    push: mockPush,
  }),
}));

describe("SearchCommand", () => {
  beforeEach(() => {
    mockPush.mockReset();
    mockSearchIssues.mockReset().mockResolvedValue({ issues: [] });
    mockSearchProjects.mockReset().mockResolvedValue({ projects: [] });
    mockRecentItems.current = [];
    mockAllIssues.current = [];

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

  it("does not show pages when no query is entered", () => {
    render(<SearchCommand />);

    expect(screen.queryByText("Pages")).not.toBeInTheDocument();
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
    expect(screen.queryByText("Projects")).not.toBeInTheDocument();
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
