// @vitest-environment node
import { expect, it } from "vitest";
import { cn } from "./utils";

it("keeps semantic text color when a role font size replaces the default", () => {
  for (const role of ["caption", "body", "title-sm"]) {
    const classes = cn("text-base text-foreground", `text-${role}`).split(" ");
    expect(classes).toContain("text-foreground");
    expect(classes).toContain(`text-${role}`);
    expect(classes).not.toContain("text-base");
  }
  expect(cn("text-foreground text-body", "text-muted-foreground")).toBe("text-body text-muted-foreground");
});
