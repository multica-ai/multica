import { describe, expect, it } from "vitest";

import {
  SIDEBAR_WRAPPER_FILL_CLASS,
  SidebarProvider,
} from "@multica/ui/components/ui/sidebar";
import { renderWithI18n } from "../test/i18n";

// The wrapper's fill is conditional, and descendants that paint an opaque layer
// over it have to match it exactly (the desktop tab flares do — #6874 shipped a
// dark square under the active tab when they didn't). SIDEBAR_WRAPPER_FILL_CLASS
// is the contract that keeps them in step. None of it is observable in jsdom, so
// these assertions cover what a rename or a class-merge conflict would silently
// break; the compiled selectors themselves are Tailwind's business.
describe("sidebar wrapper fill contract", () => {
  const variable = /^bg-\((--[a-z-]+)\)$/.exec(SIDEBAR_WRAPPER_FILL_CLASS)?.[1];

  function wrapperOf(className?: string) {
    const { container } = renderWithI18n(<SidebarProvider className={className} />);
    return container.querySelector<HTMLElement>("[data-slot='sidebar-wrapper']")!;
  }

  it("publishes the variable that SIDEBAR_WRAPPER_FILL_CLASS reads", () => {
    expect(variable).toBeDefined();
    expect(wrapperOf().className).toContain(`${variable}:var(--sidebar)`);
  });

  // Paint and variable must carry the identical condition. If one branch grows a
  // condition the other lacks, the variable starts naming a color the wrapper is
  // not painted with, which is the original bug in a new place.
  it("gates the inset fill and the inset variable on the same condition", () => {
    const classes = wrapperOf().className.split(/\s+/);
    const insetBranch = classes.filter((c) => c.startsWith("has-data-[variant=inset]:"));

    expect(insetBranch).toContain("has-data-[variant=inset]:bg-sidebar");
    expect(insetBranch).toContain(`has-data-[variant=inset]:[${variable}:var(--sidebar)]`);
  });

  // tailwind-merge drops same-group utilities, and the consumer's non-inset pair
  // arrives through the same cn() call as the inset pair above. All four have to
  // survive: lose the variable and the flares fall back to transparent, lose the
  // background and the wrapper does.
  it("keeps a consumer's non-inset fill alongside the inset branch", () => {
    const classes = wrapperOf(
      `flex-1 bg-app-shell [${variable}:var(--app-shell)]`,
    ).className.split(/\s+/);

    expect(classes).toEqual(
      expect.arrayContaining([
        "bg-app-shell",
        `[${variable}:var(--app-shell)]`,
        "has-data-[variant=inset]:bg-sidebar",
        `has-data-[variant=inset]:[${variable}:var(--sidebar)]`,
      ]),
    );
  });
});
