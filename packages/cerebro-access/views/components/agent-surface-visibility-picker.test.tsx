import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AgentSurfaceVisibilityPicker } from "./agent-surface-visibility-picker";

// TECH-3704: selecting "Advanced — per surface" used to seed an all-hidden map
// and save it, which the preset classifier reads back as "hidden" — so the
// radio snapped to Skjult and the per-surface panel never opened. The mode must
// be held in UI state, not derived from the saved value.
describe("AgentSurfaceVisibilityPicker", () => {
  it("opens the per-surface panel when Advanced is selected from Visible", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<AgentSurfaceVisibilityPicker value={null} onChange={onChange} />);

    await user.click(screen.getByLabelText(/Surface visibility/));
    // panel hidden before choosing Advanced
    expect(screen.queryByLabelText(/Visible on/)).toBeNull();

    await user.click(screen.getByText("Advanced — per surface"));

    // all four per-surface switches now render
    expect(screen.getAllByLabelText(/Visible on/)).toHaveLength(4);
    // opening Advanced alone must not persist anything (no misleading "saved")
    expect(onChange).not.toHaveBeenCalled();
  });

  it("opens the panel from the Skjult (all-hidden) state too", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <AgentSurfaceVisibilityPicker
        value={{ lists: false, mention: false, chat: false, channels: false }}
        onChange={onChange}
      />,
    );

    await user.click(screen.getByLabelText(/Surface visibility/));
    await user.click(screen.getByText("Advanced — per surface"));

    expect(screen.getAllByLabelText(/Visible on/)).toHaveLength(4);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("persists a surface toggle made from advanced mode", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<AgentSurfaceVisibilityPicker value={null} onChange={onChange} />);

    await user.click(screen.getByLabelText(/Surface visibility/));
    await user.click(screen.getByText("Advanced — per surface"));
    await user.click(
      screen.getByLabelText("Visible on Agent lists & assignee picker"),
    );

    expect(onChange).toHaveBeenCalledTimes(1);
  });
});
