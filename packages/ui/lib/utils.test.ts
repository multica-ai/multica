import { describe, expect, it } from "vitest";
import { cn } from "./utils";

describe("cn", () => {
  it.each([
    ["text-muted-foreground text-body", "text-muted-foreground text-body"],
    ["text-site-inverse-ink text-site-nav", "text-site-inverse-ink text-site-nav"],
  ])("keeps a colour and a role-named size together: %s", (input, expected) => {
    expect(cn(input)).toBe(expected);
  });

  it("lets a later size replace an earlier one", () => {
    expect(cn("text-site-nav", "text-site-body")).toBe("text-site-body");
  });
});
