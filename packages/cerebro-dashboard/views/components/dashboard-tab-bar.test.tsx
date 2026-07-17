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
	it("shows the five approved Dashboard tabs", () => {
		render(<DashboardTabBar showAIImpact />);
		for (const name of ["Overview", "Runs", "Messages", "AI Impact", "People"]) expect(screen.getByRole("button", { name })).toBeTruthy();
	});

	it("hides AI Impact when its workspace flag is off", () => {
		render(<DashboardTabBar showAIImpact={false} />);
		expect(screen.queryByRole("button", { name: "AI Impact" })).toBeNull();
		expect(screen.getByRole("button", { name: "People" })).toBeTruthy();
	});

  it("uses the approved purple active marker", () => {
    useDashboardStore.getState().setTab("runs");
    render(<DashboardTabBar />);

    expect(screen.getByRole("button", { name: "Runs" }).className).toContain("border-[#6557d8]");
  });
});
