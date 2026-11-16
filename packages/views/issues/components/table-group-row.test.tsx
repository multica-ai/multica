import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { IssueTableGroupRow } from "./table-view";

describe("IssueTableGroupRow", () => {
  it("keeps full-width row controls anchored during horizontal scrolling", async () => {
    const user = userEvent.setup();
    const onToggle = vi.fn();
    renderWithI18n(
      <table>
        <tbody>
          <IssueTableGroupRow
            group={{
              kind: "group",
              key: "status:backlog",
              label: "Backlog",
              count: 13,
              collapsed: false,
            }}
            colSpan={3}
            onToggle={onToggle}
          />
        </tbody>
      </table>,
    );

    const group = screen.getByRole("button", { name: /Backlog\s*13/ });
    expect(group).toHaveClass("sticky", "left-4", "w-fit");

    await user.click(group);
    expect(onToggle).toHaveBeenCalledOnce();
  });
});
