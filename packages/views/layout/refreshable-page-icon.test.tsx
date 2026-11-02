import { act, cleanup, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { RefreshablePageIcon } from "./refreshable-page-icon";

describe("RefreshablePageIcon", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  const icon = (refreshing: boolean) => (
    <RefreshablePageIcon refreshing={refreshing}>
      <svg aria-label="Issues icon" />
    </RefreshablePageIcon>
  );

  it("keeps the title icon until 300ms, then restores it immediately on completion", () => {
    const { rerender } = renderWithI18n(icon(false));
    const slot = screen.getByLabelText("Issues icon").parentElement;
    rerender(icon(true));
    act(() => vi.advanceTimersByTime(299));
    expect(screen.getByLabelText("Issues icon")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();

    act(() => vi.advanceTimersByTime(1));
    expect(screen.queryByLabelText("Issues icon")).not.toBeInTheDocument();
    expect(screen.getByRole("status").parentElement).toBe(slot);
    expect(slot).toHaveClass("size-4", "shrink-0");
    expect(screen.getByRole("status")).toHaveClass("motion-reduce:animate-none");

    rerender(icon(false));
    expect(screen.getByLabelText("Issues icon")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("cancels fast refreshes and starts a fresh delay for the next request", () => {
    const { rerender } = renderWithI18n(icon(true));
    act(() => vi.advanceTimersByTime(200));
    rerender(icon(false));
    act(() => vi.advanceTimersByTime(500));
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    rerender(icon(true));
    act(() => vi.advanceTimersByTime(299));
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    act(() => vi.advanceTimersByTime(1));
    expect(screen.getByRole("status")).toBeInTheDocument();
  });
});
