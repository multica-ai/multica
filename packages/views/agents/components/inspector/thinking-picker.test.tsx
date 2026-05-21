// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import type { RuntimeModelThinkingLevel } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import enIssues from "../../../locales/en/issues.json";

import { ThinkingPicker } from "./thinking-picker";

const TEST_RESOURCES = {
  en: { common: enCommon, agents: enAgents, issues: enIssues },
};

const CODEX_LEVELS: RuntimeModelThinkingLevel[] = [
  { value: "minimal", label: "Minimal", description: "Fast, light reasoning" },
  { value: "low", label: "Low" },
  { value: "medium", label: "Medium" },
  { value: "high", label: "High" },
];

function renderPicker(props: Partial<React.ComponentProps<typeof ThinkingPicker>> = {}) {
  const onChange = vi.fn();
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ThinkingPicker
        value=""
        levels={CODEX_LEVELS}
        defaultLevel="medium"
        canEdit
        onChange={onChange}
        {...props}
      />
    </I18nProvider>,
  );
  return { ...utils, onChange };
}

describe("ThinkingPicker", () => {
  beforeEach(() => {
    cleanup();
  });
  afterEach(() => {
    cleanup();
  });

  it('renders "Default" when value is empty', () => {
    renderPicker({ value: "" });
    // The trigger and the tooltip both carry the label.
    expect(screen.getAllByText("Default").length).toBeGreaterThan(0);
  });

  it("renders the matching level label when value is set", () => {
    renderPicker({ value: "high" });
    expect(screen.getAllByText("High").length).toBeGreaterThan(0);
  });

  it("renders the raw token when the saved value is no longer in the catalog", () => {
    // Simulates a model swap that dropped the option the user previously
    // picked — we still surface what's persisted so the user can clear it,
    // rather than silently showing "Default".
    renderPicker({ value: "xhigh", levels: CODEX_LEVELS });
    expect(screen.getAllByText("xhigh").length).toBeGreaterThan(0);
  });

  it("renders a static read-only display when canEdit=false and exposes no popover trigger", () => {
    renderPicker({ value: "low", canEdit: false });
    expect(screen.getByText("Low")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("calls onChange with the picked value and skips when the user re-picks the current value", () => {
    const { onChange } = renderPicker({ value: "low" });
    fireEvent.click(screen.getByRole("button"));

    // Picking a new level fires onChange with the runtime-native value.
    fireEvent.click(screen.getByText("High"));
    expect(onChange).toHaveBeenCalledWith("high");

    // Re-opening and clicking the already-selected value is a no-op so we
    // don't enqueue a redundant PATCH. The trigger also reads "Low", so
    // there are two matches in the DOM — target the listbox item by
    // selecting the option button explicitly.
    onChange.mockClear();
    fireEvent.click(screen.getByRole("button"));
    const lowOption = screen
      .getAllByRole("button")
      .find((b) => b.getAttribute("data-picker-item") !== null && b.textContent?.includes("Low"));
    expect(lowOption).toBeDefined();
    fireEvent.click(lowOption!);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("clears to empty string via the footer button when a value is set", () => {
    const { onChange } = renderPicker({ value: "high" });
    fireEvent.click(screen.getByRole("button"));
    // Footer copy resolves through i18n — match a substring so we don't
    // pin to the exact translated wording.
    const clearButton = screen.getByTitle(/Clear and fall back/i);
    fireEvent.click(clearButton);
    expect(onChange).toHaveBeenCalledWith("");
  });

  it("does not render the clear button when value is already empty", () => {
    renderPicker({ value: "" });
    fireEvent.click(screen.getByRole("button"));
    expect(screen.queryByTitle(/Clear and fall back/i)).toBeNull();
  });
});
