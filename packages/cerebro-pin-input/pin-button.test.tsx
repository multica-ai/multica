import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PinButton } from "./pin-button";

describe("PinButton", () => {
  it("renders the pin label when unpinned and exposes aria-pressed=false", () => {
    render(
      <PinButton
        isPinned={false}
        onToggle={() => {}}
        pinLabel="Pin to viewport"
        unpinLabel="Unpin from viewport"
      />,
    );
    const btn = screen.getByRole("button");
    expect(btn).toHaveAttribute("aria-pressed", "false");
    expect(btn).toHaveAttribute("aria-label", "Pin to viewport");
  });

  it("renders the unpin label when pinned and exposes aria-pressed=true", () => {
    render(
      <PinButton
        isPinned
        onToggle={() => {}}
        pinLabel="Pin to viewport"
        unpinLabel="Unpin from viewport"
      />,
    );
    const btn = screen.getByRole("button");
    expect(btn).toHaveAttribute("aria-pressed", "true");
    expect(btn).toHaveAttribute("aria-label", "Unpin from viewport");
  });

  it("calls onToggle when clicked", async () => {
    const user = userEvent.setup();
    const onToggle = vi.fn();
    render(
      <PinButton
        isPinned={false}
        onToggle={onToggle}
        pinLabel="Pin"
        unpinLabel="Unpin"
      />,
    );
    await user.click(screen.getByRole("button"));
    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});
