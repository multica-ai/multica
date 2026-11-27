import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { IconOtherOptionCard } from "./icon-option-card";

const onConfirm = vi.fn();
const onOtherChange = vi.fn();
const onSelect = vi.fn();

function renderOther(value = "yanjiu"): HTMLInputElement {
  onConfirm.mockReset();
  onOtherChange.mockReset();
  onSelect.mockReset();
  render(
    <IconOtherOptionCard
      icon={<span>✦</span>}
      label="Other"
      selected
      onSelect={onSelect}
      otherValue={value}
      onOtherChange={onOtherChange}
      onConfirm={onConfirm}
      placeholder="Your role"
    />,
  );
  return screen.getByRole("textbox", { name: "Your role" });
}

describe("IconOtherOptionCard", () => {
  it.each([
    ["standard composition signal", { isComposing: true, keyCode: 13 }],
    ["Safari composition signal", { isComposing: false, keyCode: 229 }],
  ])("does not confirm Enter with the %s", (_name, eventInit) => {
    const input = renderOther();

    fireEvent.keyDown(input, { key: "Enter", ...eventInit });

    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("confirms a non-empty value on Enter after composition", () => {
    const input = renderOther("研究");

    fireEvent.keyDown(input, { key: "Enter", isComposing: false, keyCode: 13 });

    expect(onConfirm).toHaveBeenCalledTimes(1);
  });
});
