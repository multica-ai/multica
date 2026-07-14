import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ChatSessionModeSelect } from "./chat-session-mode-select";

describe("ChatSessionModeSelect", () => {
  it("shows all fixed modes and persists the selected canonical value", async () => {
    const onChange = vi.fn();
    render(<ChatSessionModeSelect mode="auto" onChange={onChange} />);

    const trigger = screen.getByRole("combobox", { name: "Session mode" });
    expect(trigger).toHaveTextContent("Auto");
    await userEvent.click(trigger);
    expect((await screen.findAllByRole("option")).map((option) => option.textContent)).toEqual([
      "Auto",
      "Plan",
      "Build",
      "Research",
      "Review",
    ]);
    await userEvent.click(screen.getByRole("option", { name: "Review" }));
    expect(onChange).toHaveBeenCalledWith("review");
  });

  it("renders the legacy default value as Build", () => {
    render(<ChatSessionModeSelect mode="default" onChange={vi.fn()} />);
    expect(screen.getByRole("combobox", { name: "Session mode" })).toHaveTextContent("Build");
  });
});
