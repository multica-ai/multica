import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  ListGrid,
  ListGridBody,
  ListGridHeader,
  ListGridRow,
} from "@multica/ui/components/ui/list-grid";

describe("ListGrid", () => {
  it("keeps a shared inherited-track fallback for Chromium before 117", () => {
    render(
      <ListGrid>
        <ListGridHeader>Header</ListGridHeader>
        <ListGridBody>
          <ListGridRow>Row</ListGridRow>
        </ListGridBody>
      </ListGrid>,
    );

    expect(screen.getAllByRole("row")).toHaveLength(2);
    for (const nestedGrid of [
      ...screen.getAllByRole("row"),
      screen.getByRole("rowgroup"),
    ]) {
      expect(nestedGrid).toHaveClass("list-grid-subgrid");
    }

    const baseCss = readFileSync(
      resolve(process.cwd(), "../ui/styles/base.css"),
      "utf8",
    );
    expect(baseCss).toContain(
      "@supports not (grid-template-columns: subgrid)",
    );
    expect(baseCss).toMatch(
      /\.list-grid-subgrid\s*\{[^}]*grid-template-columns:\s*inherit;[^}]*column-gap:\s*inherit;/s,
    );
  });
});
