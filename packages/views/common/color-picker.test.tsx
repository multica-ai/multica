// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../test/i18n";
import { ColorPickerPanel, COLOR_PICKER_PRESETS } from "./color-picker";

afterEach(cleanup);

describe("ColorPickerPanel", () => {
  it("shows the current color in the hex input and marks the matching preset", () => {
    renderWithI18n(<ColorPickerPanel value="#3b82f6" onChange={() => {}} />);
    expect(screen.getByLabelText("Hex color")).toHaveValue("#3B82F6");
    expect(screen.getByLabelText("#3b82f6")).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  it("emits the preset color when a palette swatch is clicked", () => {
    const onChange = vi.fn();
    renderWithI18n(<ColorPickerPanel value="#3b82f6" onChange={onChange} />);
    fireEvent.click(screen.getByLabelText("#ec4899"));
    expect(onChange).toHaveBeenCalledWith("#ec4899");
  });

  it("emits a valid random color different from the current one", () => {
    const onChange = vi.fn();
    renderWithI18n(<ColorPickerPanel value="#3b82f6" onChange={onChange} />);
    fireEvent.click(screen.getByRole("button", { name: "Random" }));
    const emitted = onChange.mock.calls[0]?.[0] as string;
    expect(emitted).toMatch(/^#[0-9a-f]{6}$/);
    expect(emitted).not.toBe("#3b82f6");
  });

  it("emits typed hex values once they are complete and valid", () => {
    const onChange = vi.fn();
    renderWithI18n(<ColorPickerPanel value="#3b82f6" onChange={onChange} />);
    const input = screen.getByLabelText("Hex color");
    fireEvent.change(input, { target: { value: "#ec48" } });
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.change(input, { target: { value: "#EC4899" } });
    expect(onChange).toHaveBeenCalledWith("#ec4899");
  });

  it("renders every default preset exactly once", () => {
    renderWithI18n(<ColorPickerPanel value="#3b82f6" onChange={() => {}} />);
    for (const preset of COLOR_PICKER_PRESETS) {
      expect(screen.getAllByLabelText(preset)).toHaveLength(1);
    }
  });

  it("uses localized labels", () => {
    renderWithI18n(<ColorPickerPanel value="#3b82f6" onChange={() => {}} />, {
      locale: "zh-Hans",
    });
    expect(screen.getByRole("button", { name: "随机" })).toBeInTheDocument();
    expect(screen.getByText("预设颜色")).toBeInTheDocument();
  });
});
