import { act, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { RealtimeConnectionStatus } from "./realtime-connection-status";

const mocks = vi.hoisted(() => ({
  connectionState: "connected",
  reconnect: vi.fn(),
}));

vi.mock("@multica/core/realtime", () => ({
  useWS: () => ({
    connectionState: mocks.connectionState,
    reconnect: mocks.reconnect,
  }),
}));

describe("RealtimeConnectionStatus", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    mocks.connectionState = "connected";
    mocks.reconnect.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not flash for a short disconnection", () => {
    mocks.connectionState = "disconnected";
    const view = renderWithI18n(<RealtimeConnectionStatus />);

    act(() => vi.advanceTimersByTime(1_999));
    expect(screen.queryByRole("status")).not.toBeInTheDocument();

    mocks.connectionState = "connected";
    view.rerender(<RealtimeConnectionStatus />);
    act(() => vi.runOnlyPendingTimers());
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("shows a reconnect action after a sustained disconnection", () => {
    mocks.connectionState = "disconnected";
    renderWithI18n(<RealtimeConnectionStatus />);

    act(() => vi.advanceTimersByTime(2_000));
    fireEvent.click(
      screen.getByRole("button", { name: "Reconnect" }),
    );

    expect(screen.getByText("Real-time updates disconnected")).toBeInTheDocument();
    expect(mocks.reconnect).toHaveBeenCalledTimes(1);
  });

  it("shows reconnecting state and disables duplicate retries", () => {
    mocks.connectionState = "reconnecting";
    renderWithI18n(<RealtimeConnectionStatus />);

    act(() => vi.advanceTimersByTime(2_000));

    expect(
      screen.getByText("Reconnecting real-time updates…"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reconnect" })).toBeDisabled();
  });
});
