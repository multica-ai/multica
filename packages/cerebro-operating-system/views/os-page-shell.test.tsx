import "@testing-library/jest-dom/vitest";

import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { OsPageShell } from "./os-page-shell";

describe("OsPageShell", () => {
  // FIR-3589 item 5: every operating-system page carries the product header.
  it("renders the page title and its actions in the header", () => {
    render(
      <OsPageShell title="Roles" subtitle="Who owns what" headerActions={<button type="button">+ Add role</button>}>
        <p>body</p>
      </OsPageShell>,
    );
    expect(screen.getByRole("heading", { level: 1, name: "Roles" })).toBeInTheDocument();
    expect(screen.getByText("Who owns what")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "+ Add role" })).toBeInTheDocument();
  });

  // FIR-3589 item 7: the body owns the scrolling, so a tall page stays reachable.
  it("puts the page body in its own scroll container", () => {
    render(<OsPageShell title="Roles"><p>body</p></OsPageShell>);
    const body = screen.getByText("body").parentElement;
    expect(body).toHaveClass("overflow-y-auto");
    expect(body).toHaveClass("min-h-0");
  });
});
