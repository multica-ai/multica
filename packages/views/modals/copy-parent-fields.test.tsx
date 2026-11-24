import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Issue, IssueProperty } from "@multica/core/types";
import enIssues from "../locales/en/issues.json";
import enModals from "../locales/en/modals.json";
import { CopyParentFields } from "./copy-parent-fields";

const parent = { id: "parent", identifier: "TEST-1", priority: "high", labels: [{ id: "label", name: "Product" }], start_date: "2026-11-05", due_date: null, properties: { customer: "Acme", budget: 100000 } } as unknown as Issue;
const properties = ["customer", "budget"].map((id) => ({ id, name: id, type: id === "budget" ? "number" : "text", config: {}, archived: false })) as IssueProperty[];
function setup(onCopy = vi.fn()) {
  const user = userEvent.setup();
  const view = render(<I18nProvider locale="en" resources={{ en: { issues: enIssues, modals: enModals } }}><CopyParentFields parent={parent} properties={properties} onCopy={onCopy} /></I18nProvider>);
  return { ...view, user, onCopy };
}

describe("CopyParentFields", () => {
  it("requires an explicit selection, applies it once, and resets on reopen", async () => {
    const { user, onCopy } = setup();
    await user.click(screen.getByRole("button", { name: "Copy parent fields" }));
    expect(screen.getByRole("button", { name: "Copy 0 fields" })).toBeDisabled();
    expect(screen.getAllByRole("checkbox").every((el) => el.getAttribute("aria-checked") === "false")).toBe(true);
    await user.click(screen.getByRole("checkbox", { name: /Priority/ }));
    await user.click(screen.getByRole("checkbox", { name: /customer/ }));
    await user.click(screen.getByRole("button", { name: "Copy 2 fields" }));
    expect(onCopy).toHaveBeenCalledExactlyOnceWith({ priority: "high", properties: { customer: "Acme" } });
    await waitFor(() => expect(screen.queryByRole("checkbox")).not.toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Copy parent fields" }));
    expect(screen.getByRole("button", { name: "Copy 0 fields" })).toBeDisabled();
  });
  it("Escape cancels without modifying the draft and restores the trigger focus", async () => {
    const { user, onCopy } = setup();
    const trigger = screen.getByRole("button", { name: "Copy parent fields" });
    await user.click(trigger);
    await user.click(screen.getByRole("checkbox", { name: /Labels/ }));
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("checkbox")).not.toBeInTheDocument());
    expect(onCopy).not.toHaveBeenCalled();
    expect(trigger).toHaveFocus();
  });
});
