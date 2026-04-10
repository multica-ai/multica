import { act } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SearchCommand } from "./search-command";
import { useSearchStore } from "./search-store";

const { mockPush, mockSearchIssues, mockSearchProjects } = vi.hoisted(() => ({
  mockPush: vi.fn(),
  mockSearchIssues: vi.fn(),
  mockSearchProjects: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    searchIssues: mockSearchIssues,
    searchProjects: mockSearchProjects,
  },
}));

vi.mock("@multica/core/issues/stores", () => ({
  useRecentIssuesStore: (selector?: (state: { items: [] }) => unknown) => {
    const state = { items: [] as [] };
    return selector ? selector(state) : state;
  },
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

    expect(mockPush).toHaveBeenCalledWith("/settings");
    expect(useSearchStore.getState().open).toBe(false);
  });
});
