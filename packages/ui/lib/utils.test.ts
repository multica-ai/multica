import { describe, expect, it } from "vitest";
import { cn } from "./utils";

describe("cn", () => {
  it.each([
    ["text-muted-foreground text-body", "text-muted-foreground text-body"],
    ["text-site-inverse-ink text-site-small", "text-site-inverse-ink text-site-small"],
  ])("keeps a colour and a role-named size together: %s", (input, expected) => {
    expect(cn(input)).toBe(expected);
  });

  it("keeps a role-named font family next to a weight", () => {
    expect(cn("font-medium font-site-display")).toBe("font-medium font-site-display");
    expect(cn("font-site-sans", "font-site-display")).toBe("font-site-display");
  });

  it("lets a later size replace an earlier one", () => {
    expect(cn("text-site-small", "text-site-body")).toBe("text-site-body");
  });
});
