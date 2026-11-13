import { describe, expect, it } from "vitest";
import type { Label } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";
import { LabelChip } from "./label-chip";

const label = (color: string) =>
  ({ id: "label-1", name: "Design", color, workspace_id: "ws-1" }) as Label;

const chip = (container: HTMLElement) => container.querySelector<HTMLElement>('[data-mc="label-chip"]')!;

describe("LabelChip", () => {
  it("paints from --label-color, not an inline background, so themes can restyle it", () => {
    const el = chip(renderWithI18n(<LabelChip label={label("#8b5cf6")} />).container);

    expect(el.style.getPropertyValue("--label-color")).toBe("#8b5cf6");
    expect(el.style.backgroundColor).toBe("");
    expect(el.className).toContain("bg-(--label-color)");
    expect(el).toHaveAttribute("data-color", "#8b5cf6");
  });

  it("sets a readable text colour as --label-foreground", () => {
    const dark = chip(renderWithI18n(<LabelChip label={label("#1e3a8a")} />).container);
    const light = chip(renderWithI18n(<LabelChip label={label("#fde68a")} />).container);

    expect(dark.style.getPropertyValue("--label-foreground")).toBe("#f9fafb");
    expect(light.style.getPropertyValue("--label-foreground")).toBe("#111827");
    expect(dark.style.color).toBe("");
    expect(dark.className).toContain("text-(--label-foreground)");
  });
});
