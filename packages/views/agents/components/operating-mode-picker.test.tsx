// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { OperatingModePicker } from "./operating-mode-picker";

describe("OperatingModePicker", () => {
  afterEach(cleanup);

  it("renders an accessible three-choice radio group", () => {
    renderWithI18n(
      <OperatingModePicker value="coding" onChange={vi.fn()} />,
    );

    expect(
      screen.getByRole("radiogroup", { name: "Operating mode" }),
    ).toBeInTheDocument();
    expect(screen.getAllByRole("radio")).toHaveLength(3);
    expect(screen.getByRole("radio", { name: /Coding/ })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(
      screen.getByText(
        "Workflow intent only. This does not grant permissions or approve tool use.",
      ),
    ).toBeInTheDocument();
  });

  it("selects with click and arrow keys using roving focus", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    renderWithI18n(
      <OperatingModePicker value="coding" onChange={onChange} />,
    );

    await user.click(screen.getByRole("radio", { name: /Operational/ }));
    expect(onChange).toHaveBeenCalledWith("operational");

    const coding = screen.getByRole("radio", { name: /Coding/ });
    coding.focus();
    await user.keyboard("{ArrowRight}");
    expect(onChange).toHaveBeenLastCalledWith("operational");
    expect(screen.getByRole("radio", { name: /Operational/ })).toHaveFocus();
  });

  it("does not change when editing is disabled", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    renderWithI18n(
      <OperatingModePicker value="coding" onChange={onChange} disabled />,
    );

    await user.click(screen.getByRole("radio", { name: /Operational/ }));
    expect(onChange).not.toHaveBeenCalled();
  });
});
