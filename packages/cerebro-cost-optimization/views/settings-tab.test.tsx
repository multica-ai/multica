import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { SavingHoldoutControl } from "./settings-tab";

// sonner's toast is a side effect we don't care about here; stub it so a parse
// error in the input doesn't try to mount a real toaster.
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

function renderControl(
  props: Partial<React.ComponentProps<typeof SavingHoldoutControl>> = {},
) {
  const onApply = vi.fn();
  render(
    <SavingHoldoutControl
      label="Model routing"
      override={undefined}
      canManage={true}
      pending={false}
      onApply={onApply}
      {...props}
    />,
  );
  return { onApply };
}

describe("SavingHoldoutControl", () => {
  it("shows the server default when the saving has no override", () => {
    renderControl({ override: undefined });
    expect(screen.getByText(/80% rollout/)).toBeInTheDocument();
    expect(screen.getByText(/server default/)).toBeInTheDocument();
  });

  it("shows the saving's override when set", () => {
    renderControl({ override: 35 });
    expect(screen.getByText(/65% rollout/)).toBeInTheDocument();
    expect(screen.getByText(/set for this workspace/)).toBeInTheDocument();
  });

  it("Full rollout (100%) applies holdout 0", () => {
    const { onApply } = renderControl({ override: 20 });
    fireEvent.click(screen.getByRole("button", { name: /full rollout/i }));
    expect(onApply).toHaveBeenCalledWith(0);
  });

  it("disables Full rollout when already at full rollout (holdout 0)", () => {
    renderControl({ override: 0 });
    const btn = screen.getByRole("button", { name: /full rollout/i });
    expect(btn).toBeDisabled();
    expect(btn).toHaveAttribute("aria-pressed", "true");
  });

  it("Save applies the clamped entered value", () => {
    const { onApply } = renderControl({ override: 20 });
    fireEvent.change(screen.getByLabelText(/holdout %/i), {
      target: { value: "150" },
    });
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
    expect(onApply).toHaveBeenCalledWith(100); // clamped to 100
  });

  it("shows Reset to default only when overridden, and it clears the override", () => {
    const { onApply } = renderControl({ override: 40 });
    fireEvent.click(screen.getByRole("button", { name: /reset to default/i }));
    expect(onApply).toHaveBeenCalledWith(null);
  });

  it("hides Reset to default when there is no override", () => {
    renderControl({ override: undefined });
    expect(
      screen.queryByRole("button", { name: /reset to default/i }),
    ).toBeNull();
  });

  it("disables all controls when the user cannot manage", () => {
    renderControl({ override: 20, canManage: false });
    expect(
      screen.getByRole("button", { name: /full rollout/i }),
    ).toBeDisabled();
    expect(screen.getByRole("button", { name: /^save$/i })).toBeDisabled();
    expect(screen.getByLabelText(/holdout %/i)).toBeDisabled();
  });
});
