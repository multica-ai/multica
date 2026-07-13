// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { SkillUsagePanel } from "./panel";

describe("SkillUsagePanel", () => {
  it("filters the dashboard from a skill row with keyboard-accessible controls", () => {
    const onSelect = vi.fn();
    render(
      <SkillUsagePanel
        rows={[{ skill_id: "s1", skill_name: "TDD", invocation_count: 4, run_count: 2, last_used_at: null }]}
        isLoading={false}
        onSelect={onSelect}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Include TDD" }));
    expect(onSelect).toHaveBeenCalledWith("TDD", "include");
    fireEvent.click(screen.getByRole("button", { name: "Exclude TDD" }));
    expect(onSelect).toHaveBeenCalledWith("TDD", "exclude");
  });
});
