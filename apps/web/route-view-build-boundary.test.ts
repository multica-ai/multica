import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const dashboardRouteFiles = [
  "(auth)/invite/[id]/page.tsx",
  "(auth)/invitations/page.tsx",
  "(auth)/login/page.tsx",
  "(auth)/onboarding/page.tsx",
  "[workspaceSlug]/(dashboard)/agents/page.tsx",
  "[workspaceSlug]/(dashboard)/agents/[id]/page.tsx",
  "[workspaceSlug]/(dashboard)/autopilots/page.tsx",
  "[workspaceSlug]/(dashboard)/autopilots/[id]/page.tsx",
  "[workspaceSlug]/(dashboard)/inbox/page.tsx",
  "[workspaceSlug]/(dashboard)/issues/page.tsx",
  "[workspaceSlug]/(dashboard)/issues/[id]/page.tsx",
  "[workspaceSlug]/(dashboard)/my-issues/page.tsx",
  "[workspaceSlug]/(dashboard)/projects/page.tsx",
  "[workspaceSlug]/(dashboard)/projects/[id]/page.tsx",
  "[workspaceSlug]/(dashboard)/runtimes/page.tsx",
  "[workspaceSlug]/(dashboard)/runtimes/[id]/page.tsx",
  "[workspaceSlug]/(dashboard)/settings/page.tsx",
  "[workspaceSlug]/(dashboard)/skills/page.tsx",
  "[workspaceSlug]/(dashboard)/skills/[id]/page.tsx",
  "[workspaceSlug]/(dashboard)/squads/page.tsx",
  "[workspaceSlug]/(dashboard)/squads/[id]/page.tsx",
  "[workspaceSlug]/(dashboard)/usage/page.tsx",
  "[workspaceSlug]/attachments/[id]/preview/page.tsx",
];

describe("route view build boundary", () => {
  it("imports dashboard page components through leaf view exports", () => {
    const barrelImports = /@multica\/views\/(?:agents|attachments|auth|autopilots\/components|dashboard|inbox|invitations|invite|issues\/components|layout|members|my-issues|onboarding|projects\/components|runtimes|settings|skills|squads)(?:["'])/;

    for (const routeFile of dashboardRouteFiles) {
      const source = readFileSync(resolve(__dirname, "app", routeFile), "utf8");
      expect(source, routeFile).not.toMatch(barrelImports);
    }
  });

  it("imports navigation primitives through leaf view exports", () => {
    const source = readFileSync(resolve(__dirname, "platform/navigation.tsx"), "utf8");

    expect(source).not.toContain("@multica/views/navigation\"");
  });
});
