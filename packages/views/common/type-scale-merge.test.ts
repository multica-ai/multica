import { describe, expect, it } from "vitest";
import { cn } from "@multica/ui/lib/utils";

/**
 * Do the type scale's role-named steps survive `cn()`?
 *
 * `text-<x>` is ambiguous in Tailwind — it can be a font size or a colour — and
 * tailwind-merge resolves it against a built-in table that only knows the
 * default sizes. Role names like `text-body` are absent from that table, so an
 * unconfigured merge files them under text-colour and then discards whichever
 * of `text-body` / `text-muted-foreground` came first as a conflicting class.
 *
 * That failure is silent and invisible in review: the source still reads
 * `text-caption text-sidebar-foreground/70`, but the element renders at the
 * inherited 16px. It shipped exactly once, on the sidebar group labels, which
 * is why the merge config is pinned by a test rather than left to inspection.
 */

const STEPS = [
  "text-micro",
  "text-caption",
  "text-label",
  "text-body",
  "text-body-lg",
  "text-title-sm",
  "text-title",
  "text-title-lg",
  "text-display-sm",
  "text-display",
] as const;

const COLOURS = [
  "text-muted-foreground",
  "text-foreground",
  "text-sidebar-foreground/70",
  "text-brand",
  "text-destructive",
] as const;

describe("type scale survives cn()", () => {
  it.each(STEPS)("%s is kept when a colour class follows it", (step) => {
    for (const colour of COLOURS) {
      const merged = cn(step, colour).split(" ");
      expect(merged, `${step} + ${colour}`).toContain(step);
      expect(merged, `${step} + ${colour}`).toContain(colour);
    }
  });

  it.each(STEPS)("%s is kept when it follows a colour class", (step) => {
    for (const colour of COLOURS) {
      const merged = cn(colour, step).split(" ");
      expect(merged, `${colour} + ${step}`).toContain(step);
      expect(merged, `${colour} + ${step}`).toContain(colour);
    }
  });

  it("still treats two steps as a conflict, last one winning", () => {
    expect(cn("text-caption", "text-body")).toBe("text-body");
    expect(cn("text-title-sm", "text-title-lg")).toBe("text-title-lg");
    // The override path components rely on: a caller passing a size in
    // className must beat the component's own default.
    expect(cn("text-body text-muted-foreground", "text-micro")).toBe(
      "text-muted-foreground text-micro",
    );
  });

  it("still treats two colours as a conflict, last one winning", () => {
    expect(cn("text-muted-foreground", "text-foreground")).toBe("text-foreground");
  });
});
