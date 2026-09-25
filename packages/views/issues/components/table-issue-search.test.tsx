/**
 * @vitest-environment jsdom
 */
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TableIssueSearch } from "./table-view";

describe("TableIssueSearch", () => {
  it("forwards typing and exposes an accessible clear action", () => {
    const onChange = vi.fn();
    const view = render(
      <TableIssueSearch
        value=""
        onChange={onChange}
        placeholder="Search title or issue ID…"
        clearLabel="Clear search"
      />,
    );

    fireEvent.change(screen.getByRole("searchbox"), {
      target: { value: "MUL-4797" },
    });
    expect(onChange).toHaveBeenLastCalledWith("MUL-4797");

    view.rerender(
      <TableIssueSearch
        value="MUL-4797"
        onChange={onChange}
        placeholder="Search title or issue ID…"
        clearLabel="Clear search"
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
    expect(onChange).toHaveBeenLastCalledWith("");
  });
});
