import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { IssueProperty, IssuePropertyValue } from "@multica/core/types";
import enIssues from "../../../locales/en/issues.json";
import { CustomPropertyValueInput } from "./custom-property-picker";

const options = Array.from({ length: 200 }, (_, i) => ({
  id: `option-${i + 1}`, name: `Service ${i + 1}`, color: "#3b82f6",
}));
const property: IssueProperty = {
  id: "p-1", workspace_id: "ws-1", name: "Services", type: "multi_select",
  config: { options }, position: 1, archived: false,
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
};

function renderPicker(count: number, type: "select" | "multi_select" = "multi_select") {
  const onChange = vi.fn();
  function Picker() {
    const [value, setValue] = useState<IssuePropertyValue | undefined>(
      type === "multi_select" ? options.slice(0, count).map((option) => option.id) : undefined,
    );
    return (
      <I18nProvider locale="en" resources={{ en: { issues: enIssues } }}>
        <CustomPropertyValueInput
          property={{ ...property, type }} value={value} defaultOpen trigger="Services"
          onChange={(next) => { onChange(next); setValue(next); }}
        />
      </I18nProvider>
    );
  }
  render(<Picker />);
  return onChange;
}

describe("large select option lists", () => {
  it("caps multi-select values at 50 while allowing removal and replacement", async () => {
    const user = userEvent.setup();
    const onChange = renderPicker(50);
    const last = await screen.findByRole("button", { name: "Service 200" });
    expect(last).toBeDisabled();
    await user.click(last);
    expect(onChange).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Service 50" }));
    expect(last).toBeEnabled();
    await user.click(last);
    expect(onChange).toHaveBeenLastCalledWith([
      ...options.slice(0, 49).map((option) => option.id), "option-200",
    ]);
    expect(screen.getByRole("button", { name: "Service 50" })).toBeDisabled();
    expect(last).toBeEnabled();
  });

  it.each(["select", "multi_select"] as const)("searches and selects the last of 200 %s options with the keyboard", async (type) => {
    const user = userEvent.setup();
    const onChange = renderPicker(0, type);
    const search = await screen.findByRole("textbox");
    await user.type(search, "SERVICE 200{Enter}");
    expect(onChange).toHaveBeenLastCalledWith(type === "select" ? "option-200" : ["option-200"]);
  });

  it("does not clear the value when a search has no matches", async () => {
    const user = userEvent.setup();
    const onChange = renderPicker(1);
    await user.type(await screen.findByRole("textbox"), "no such service{Enter}");
    expect(screen.queryByRole("button", { name: "Service 1" })).not.toBeInTheDocument();
    expect(onChange).not.toHaveBeenCalled();
  });

  it("resets the search when reopened after selection", async () => {
    const user = userEvent.setup();
    renderPicker(0, "select");
    await user.type(await screen.findByRole("textbox"), "Service 200{Enter}");
    await user.click(screen.getByRole("button", { name: "Services" }));
    expect(await screen.findByRole("textbox")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Service 1" })).toBeInTheDocument();
  });
});
