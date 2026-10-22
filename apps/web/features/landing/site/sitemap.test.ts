// @vitest-environment node
import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { RESERVED_SLUGS } from "@multica/core/paths";
import {
  PRODUCT_GROUPS,
  SOLUTION_GROUPS,
  TOP_LINKS,
  buildFooterGroups,
  buildMenus,
  siteRoutes,
  type SiteLink,
} from "./sitemap";

const landingRoutes = resolve(__dirname, "../../../app/(landing)");

const internalLinks = (links: SiteLink[]) => links.filter((link) => !link.external);
const navLinks = () => [
  ...TOP_LINKS,
  ...buildMenus("/docs").flatMap((menu) => menu.groups.flatMap((group) => group.links)),
  ...buildFooterGroups("/docs").flatMap((group) => group.links),
];

describe("site map", () => {
  it("points every internal menu and footer link at a served route", () => {
    const routes = new Set(siteRoutes());
    for (const link of internalLinks(navLinks())) {
      expect(routes, `${link.label} → ${link.href}`).toContain(link.href);
    }
  });

  it("backs every route with a page under app/(landing)", () => {
    for (const route of siteRoutes()) {
      const segments = route.split("/").filter(Boolean);
      const dynamic = segments.length === 2 ? [segments[0]!, "[slug]"] : segments;
      expect(existsSync(resolve(landingRoutes, ...dynamic, "page.tsx")), route).toBe(true);
    }
  });

  it("reserves every top-level segment so no workspace slug can shadow it", () => {
    const roots = new Set(siteRoutes().map((route) => route.split("/")[1]).filter(Boolean));
    for (const root of roots) {
      expect(RESERVED_SLUGS.has(root!), root).toBe(true);
    }
  });

  it("keeps page slugs unique within each section", () => {
    for (const groups of [PRODUCT_GROUPS, SOLUTION_GROUPS]) {
      const slugs = groups.flatMap((group) => group.pages.map((page) => page.slug));
      expect(new Set(slugs).size).toBe(slugs.length);
    }
  });

  it.each(["/docs", "/docs/zh", "/docs/ja"])("links documentation to the %s docs", (docsHref) => {
    const resources = buildMenus(docsHref).find((menu) => menu.id === "resources")!;
    const docs = resources.groups.flatMap((group) => group.links).find((link) => link.label === "Documentation");
    expect(docs?.href).toBe(docsHref);
    const footerDocs = buildFooterGroups(docsHref)
      .flatMap((group) => group.links)
      .find((link) => link.label === "Documentation");
    expect(footerDocs?.href).toBe(docsHref);
  });
});
