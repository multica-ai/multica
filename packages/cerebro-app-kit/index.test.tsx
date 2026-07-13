// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AppButton, AppField, AppFormCard, AppTable } from "./index";

describe("cerebro app kit", () => {
  it("provides accessible form primitives with Multica-neutral English labels", () => {
    render(<AppFormCard title="Format ingredients" description="Paste an ingredient list"><AppField label="Ingredients" name="ingredients" /></AppFormCard>);
    expect(screen.getByRole("heading", { name: "Format ingredients" })).toBeInTheDocument();
    expect(screen.getByLabelText("Ingredients")).toHaveAttribute("name", "ingredients");
  });

  it("renders buttons and responsive table semantics", () => {
    render(<><AppButton>Run formatter</AppButton><AppTable columns={[{ key: "sku", label: "SKU" }]} rows={[{ sku: "A-1" }]} /></>);
    expect(screen.getByRole("button", { name: "Run formatter" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "SKU" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "A-1" })).toHaveAttribute("data-label", "SKU");
  });
});
