import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocalConversationSearch } from "./local-conversation-search";

const labels = { search: "Search conversations", noResults: "No matching conversations", loading: "Loading...", retry: "Retry" };
const entry = { id: "one", title: "First conversation", preview: "Latest reply", provider: "claude" as const, cwd: "/work", pinned: false, archived: false, projectId: null, running: false, waitingForAnswer: false, backgroundTasks: 0, turnCount: 1, lastTurnSentAt: "2026-11-06T00:00:00Z", updatedAt: "2026-11-06" };
const props = () => ({ sessions: [entry], projects: [], archived: false, pending: false, error: null, retry: vi.fn(), select: vi.fn(), labels, unassigned: "Unassigned" });
async function open() {
  fireEvent.click(screen.getByRole("button", { name: labels.search }));
  return screen.findByRole("combobox", { name: labels.search });
}
beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("local conversation search", () => {
  it("opens from the toolbar, clears a dismissed query, and selects a result", async () => {
    const value = props();
    render(<LocalConversationSearch {...value} />);
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    const input = await open();
    await waitFor(() => expect(input).toHaveFocus());
    fireEvent.change(input, { target: { value: "Missing" } });
    expect(screen.getByRole("status")).toHaveTextContent(labels.noResults);
    fireEvent.keyDown(input, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const reopened = await open();
    expect(reopened).toHaveValue("");
    fireEvent.change(reopened, { target: { value: "Latest reply" } });
    fireEvent.click(screen.getByRole("option", { name: /First conversation/ }));
    expect(value.select).toHaveBeenCalledWith("one", "claude");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("searches archives separately and keeps removed project conversations unassigned", async () => {
    const value = { ...props(), sessions: [entry, { ...entry, id: "archived", title: "Archived conversation", archived: true }], archived: true };
    const { rerender } = render(<LocalConversationSearch {...value} />);
    await open();
    expect(screen.getByRole("option", { name: /Archived conversation/ })).toBeVisible();
    expect(screen.queryByRole("option", { name: /First conversation/ })).not.toBeInTheDocument();
    rerender(<LocalConversationSearch {...value} archived={false} />);
    expect(screen.getByRole("option", { name: /First conversation/ })).toBeVisible();
    expect(screen.getByText("Unassigned")).toBeVisible();
  });

  it("shows loading and an actionable load error instead of stale results", async () => {
    const value = props();
    const { rerender } = render(<LocalConversationSearch {...value} pending />);
    await open();
    expect(screen.getByRole("status")).toHaveTextContent(labels.loading);
    expect(screen.queryByRole("option")).not.toBeInTheDocument();
    rerender(<LocalConversationSearch {...value} error={new Error("Runtime unavailable")} />);
    expect(screen.getByRole("alert")).toHaveTextContent("Runtime unavailable");
    fireEvent.click(screen.getByRole("button", { name: labels.retry }));
    expect(value.retry).toHaveBeenCalledOnce();
    rerender(<LocalConversationSearch {...value} />);
    expect(screen.getByRole("option")).toBeVisible();
  });
});
