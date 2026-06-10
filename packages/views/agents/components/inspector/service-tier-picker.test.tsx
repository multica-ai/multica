// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../../locales/en/agents.json";
import enCommon from "../../../locales/en/common.json";
import enIssues from "../../../locales/en/issues.json";
import { ServiceTierPicker } from "./service-tier-picker";

const TEST_RESOURCES = {
  en: { agents: enAgents, common: enCommon, issues: enIssues },
};

function renderPicker(
  props: Partial<React.ComponentProps<typeof ServiceTierPicker>> = {},
) {
  const onChange = vi.fn();
  const utils = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ServiceTierPicker
        value=""
        canEdit
        onChange={onChange}
        {...props}
      />
    </I18nProvider>,
  );
  return { ...utils, onChange };
}

describe("ServiceTierPicker", () => {
  beforeEach(() => {
    cleanup();
  });

  afterEach(() => {
    cleanup();
  });

  it("renders follow-local config for the empty service tier", () => {
    renderPicker({ value: "" });
    expect(screen.getByText("Follow local Codex config")).toBeInTheDocument();
  });

  it("calls onChange with the selected Codex service tier", () => {
    const { onChange } = renderPicker({ value: "" });
    fireEvent.click(screen.getByRole("button"));
    fireEvent.click(screen.getByText("Fast"));

    expect(onChange).toHaveBeenCalledWith("fast");
  });

  it("can clear a persisted tier back to the empty follow-local sentinel", () => {
    const { onChange } = renderPicker({ value: "fast" });
    fireEvent.click(screen.getByRole("button"));
    fireEvent.click(screen.getByText("Follow local Codex config"));

    expect(onChange).toHaveBeenCalledWith("");
  });

  it("skips redundant updates when the current tier is re-picked", () => {
    const { onChange } = renderPicker({ value: "default" });
    fireEvent.click(screen.getByRole("button"));
    const standardOption = screen
      .getAllByRole("button")
      .find((button) =>
        button.getAttribute("data-picker-item") !== null &&
        button.textContent?.includes("Standard"),
      );
    expect(standardOption).toBeDefined();
    fireEvent.click(standardOption!);

    expect(onChange).not.toHaveBeenCalled();
  });

  it("renders a static read-only display when canEdit=false", () => {
    renderPicker({ value: "fast", canEdit: false });

    expect(screen.getByText("Fast")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
