import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

// Mock next/navigation
const mockPush = vi.fn();
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush }),
}));

// Mock the search API
const mockSearch = vi.fn();
vi.mock("@/shared/api", () => ({
  api: {
    search: (...args: unknown[]) => mockSearch(...args),
  },
}));

// Mock issue feature components
vi.mock("@/features/issues", () => ({
  StatusIcon: ({ status }: { status: string }) => (
    <span data-testid={`status-icon-${status}`} />
  ),
  PriorityIcon: ({ priority }: { priority: string }) => (
    <span data-testid={`priority-icon-${priority}`} />
  ),
}));

import { SearchCommand } from "./search-command";
import { useSearchCommandStore } from "../stores/search-command-store";

describe("SearchCommand", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    useSearchCommandStore.setState({ open: false });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("opens on Cmd+K keyboard shortcut", async () => {
    render(<SearchCommand />);

    // Dialog should not be visible initially
    expect(screen.queryByPlaceholderText("Search issues...")).not.toBeInTheDocument();

    // Simulate Cmd+K
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "k", metaKey: true }),
    );

    await waitFor(() => {
      expect(screen.getByPlaceholderText("Search issues...")).toBeInTheDocument();
    });
  });

  it("displays search results returned by the API", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });

    mockSearch.mockResolvedValue({
      issues: [
        {
          id: "issue-1",
          number: 1,
          identifier: "MUL-1",
          title: "Fix login bug",
          status: "in_progress",
          priority: "high",
          assignee_type: "member",
          assignee_id: "user-1",
        },
        {
          id: "issue-2",
          number: 2,
          identifier: "MUL-2",
          title: "Add search feature",
          status: "todo",
          priority: "medium",
          assignee_type: null,
          assignee_id: null,
        },
      ],
    });

    render(<SearchCommand />);

    // Open the dialog
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "k", metaKey: true }),
    );

    await waitFor(() => {
      expect(screen.getByPlaceholderText("Search issues...")).toBeInTheDocument();
    });

    // Type a search query
    const input = screen.getByPlaceholderText("Search issues...");
    await user.type(input, "bug");

    // Advance the debounce timer
    vi.advanceTimersByTime(300);

    await waitFor(() => {
      expect(screen.getByText("Fix login bug")).toBeInTheDocument();
      expect(screen.getByText("Add search feature")).toBeInTheDocument();
    });

    // Check identifiers are shown
    expect(screen.getByText("MUL-1")).toBeInTheDocument();
    expect(screen.getByText("MUL-2")).toBeInTheDocument();

    // Check status and priority icons
    expect(screen.getByTestId("status-icon-in_progress")).toBeInTheDocument();
    expect(screen.getByTestId("status-icon-todo")).toBeInTheDocument();
    expect(screen.getByTestId("priority-icon-high")).toBeInTheDocument();
    expect(screen.getByTestId("priority-icon-medium")).toBeInTheDocument();
  });

  it("shows empty state when no results", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });

    mockSearch.mockResolvedValue({ issues: [] });

    render(<SearchCommand />);

    // Open the dialog
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "k", metaKey: true }),
    );

    await waitFor(() => {
      expect(screen.getByPlaceholderText("Search issues...")).toBeInTheDocument();
    });

    // Type a search query
    const input = screen.getByPlaceholderText("Search issues...");
    await user.type(input, "nonexistent");

    // Advance the debounce timer
    vi.advanceTimersByTime(300);

    await waitFor(() => {
      expect(screen.getByText("No results found.")).toBeInTheDocument();
    });
  });

  it("navigates to issue on selection", async () => {
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });

    mockSearch.mockResolvedValue({
      issues: [
        {
          id: "issue-nav-1",
          number: 42,
          identifier: "MUL-42",
          title: "Navigation test issue",
          status: "todo",
          priority: "medium",
          assignee_type: null,
          assignee_id: null,
        },
      ],
    });

    render(<SearchCommand />);

    // Open the dialog
    document.dispatchEvent(
      new KeyboardEvent("keydown", { key: "k", metaKey: true }),
    );

    await waitFor(() => {
      expect(screen.getByPlaceholderText("Search issues...")).toBeInTheDocument();
    });

    // Type a search query
    const input = screen.getByPlaceholderText("Search issues...");
    await user.type(input, "Navigation");

    // Advance the debounce timer
    vi.advanceTimersByTime(300);

    await waitFor(() => {
      expect(screen.getByText("Navigation test issue")).toBeInTheDocument();
    });

    // Click the result
    await user.click(screen.getByText("Navigation test issue"));

    expect(mockPush).toHaveBeenCalledWith("/issues/issue-nav-1");
  });
});
