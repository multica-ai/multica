// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { useDashboardStore } from "../../core/store";
import { DashboardTabBar } from "./dashboard-tab-bar";

afterEach(() => {
  cleanup();
  useDashboardStore.getState().reset();
});

describe("DashboardTabBar", () => {
  it("uses the approved purple active marker", () => {
    useDashboardStore.getState().setTab("runs");
    render(<DashboardTabBar />);

    expect(screen.getByRole("button", { name: "Runs" }).className).toContain("border-[#6557d8]");
  });
});
