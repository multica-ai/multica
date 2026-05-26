import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { ConfirmTimerSwitchDialog } from "./ConfirmTimerSwitchDialog";

describe("ConfirmTimerSwitchDialog", () => {
  it("disables both actions while a switch request is in flight", () => {
    render(
      <ConfirmTimerSwitchDialog
        open
        isLoading
        onCancel={vi.fn()}
        onConfirm={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: "Keep current timer" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Confirm switch" })).toBeDisabled();
  });
});
