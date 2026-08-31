// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getActiveTab, useTabStore } from "@multica/core/tabs";
import { SessionHistoryControls } from "./session-history-controls";

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (r: { tab_history: Record<string, string> }) => string) =>
      sel({
        tab_history: {
          back: "Go back",
          forward: "Go forward",
        },
      }),
  }),
}));

beforeEach(() => {
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme", "/acme/issues");
});

describe("SessionHistoryControls", () => {
  it("disables both directions on a fresh tab", () => {
    render(<SessionHistoryControls />);
    expect(screen.getByRole("button", { name: "Go back" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Go forward" })).toBeDisabled();
  });

  it("walks the tab's virtual history after an in-tab push", async () => {
    const user = userEvent.setup();
    useTabStore.getState().navigateActiveSession("/acme/projects");
    render(<SessionHistoryControls />);

    const back = screen.getByRole("button", { name: "Go back" });
    expect(back).toBeEnabled();
    await user.click(back);
    expect(getActiveTab(useTabStore.getState())?.url).toBe("/acme/issues");

    const forward = screen.getByRole("button", { name: "Go forward" });
    expect(forward).toBeEnabled();
    await user.click(forward);
    expect(getActiveTab(useTabStore.getState())?.url).toBe("/acme/projects");
  });

  it("keeps a coarse-pointer hit slop on the buttons", () => {
    render(<SessionHistoryControls />);
    expect(screen.getByRole("button", { name: "Go back" }).className).toContain(
      "before:inset-[-8px]",
    );
    expect(screen.getByRole("button", { name: "Go back" }).className).toContain(
      "[@media(hover:hover)_and_(pointer:fine)]:before:content-none",
    );
  });
});
