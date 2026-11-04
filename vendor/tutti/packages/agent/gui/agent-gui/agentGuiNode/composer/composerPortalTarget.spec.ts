import { describe, expect, it } from "vitest";
import { getPortalContainer } from "@tutti-os/ui-system/portal-container";
import { resolveComposerPortalTarget } from "./composerPortalTarget";

describe("resolveComposerPortalTarget", () => {
  it("uses the body for viewport-positioned menus inside a translated boundary", () => {
    const boundary = document.createElement("div");
    boundary.setAttribute("data-slot", "viewport-menu-boundary");
    boundary.setAttribute("data-viewport-menu-portal-target", "body");
    const anchor = document.createElement("div");
    boundary.appendChild(anchor);
    document.body.appendChild(boundary);

    expect(resolveComposerPortalTarget(anchor)).toBe(getPortalContainer());

    boundary.remove();
  });

  it("keeps ordinary boundary-local menus inside their boundary", () => {
    const boundary = document.createElement("div");
    boundary.setAttribute("data-slot", "viewport-menu-boundary");
    const anchor = document.createElement("div");
    boundary.appendChild(anchor);
    document.body.appendChild(boundary);

    expect(resolveComposerPortalTarget(anchor)).toBe(boundary);

    boundary.remove();
  });
});
