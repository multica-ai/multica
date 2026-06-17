import type { ReactNode } from "react";
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AgentSurfaceVisibilityPicker } from "../views/components/agent-surface-visibility-picker";

// Popover is a Base UI portal that's awkward in jsdom — render it inline so the
// radio + per-surface panel are always in the document and observable.
vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  PopoverTrigger: ({ children, ...props }: { children: ReactNode }) => (
    <button type="button" {...props}>
      {children}
    </button>
  ),
  PopoverContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

const SURFACE_LABELS = [
  "Agent lists & assignee picker",
  "@-mention autocomplete",
  "Chat / direct message picker",
  "Channel member pickers",
] as const;

describe("AgentSurfaceVisibilityPicker", () => {
  it("opens the per-surface panel when Advanced is selected (TECH-3670 regression)", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<AgentSurfaceVisibilityPicker value={null} onChange={onChange} />);

    // Panel is hidden under the default "Visible to everyone" preset.
    expect(screen.queryByText(SURFACE_LABELS[0])).not.toBeInTheDocument();

    const radios = screen.getAllByRole("radio");
    await user.click(radios[2]!); // "Advanced — per surface"

    // The bug: before the fix the all-hidden seed re-classified as "hidden",
    // the panel never opened, and the radio snapped back to Skjult. Now the
    // four per-surface switches must be visible.
    for (const label of SURFACE_LABELS) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
    expect(screen.getAllByRole("switch")).toHaveLength(4);

    // Merely entering Advanced must NOT persist anything — picking Advanced
    // should not silently hide the agent everywhere.
    expect(onChange).not.toHaveBeenCalled();
  });

  it("persists only the toggled surface, keeping the panel open", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<AgentSurfaceVisibilityPicker value={null} onChange={onChange} />);

    await user.click(screen.getAllByRole("radio")[2]!); // Advanced
    await user.click(screen.getAllByRole("switch")[0]!); // turn off "lists"

    expect(onChange).toHaveBeenCalledWith({ lists: false });
    // Panel stays open after a toggle.
    expect(screen.getByText(SURFACE_LABELS[0])).toBeInTheDocument();
  });

  it("shows the per-surface panel for a stored mixed (custom) map", () => {
    render(
      <AgentSurfaceVisibilityPicker value={{ lists: false }} onChange={vi.fn()} />,
    );
    expect(screen.getByText(SURFACE_LABELS[0])).toBeInTheDocument();
  });

  it("writes the all-hidden map when Skjult is picked", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<AgentSurfaceVisibilityPicker value={null} onChange={onChange} />);

    await user.click(screen.getAllByRole("radio")[1]!); // Skjult (hidden)
    expect(onChange).toHaveBeenCalledWith({
      lists: false,
      mention: false,
      chat: false,
      channels: false,
    });
  });

  it("writes an empty map when Visible is picked", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    // Start hidden so picking "Visible" is a real change, not a no-op re-click.
    render(
      <AgentSurfaceVisibilityPicker
        value={{ lists: false, mention: false, chat: false, channels: false }}
        onChange={onChange}
      />,
    );

    await user.click(screen.getAllByRole("radio")[0]!); // Visible to everyone
    expect(onChange).toHaveBeenCalledWith({});
  });
});
