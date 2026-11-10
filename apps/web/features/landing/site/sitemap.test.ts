// @vitest-environment node
import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { RESERVED_SLUGS } from "@multica/core/paths";
import { enSiteCopy } from "../i18n/site-en";
import {
  PRODUCT_GROUPS,
  SOLUTION_GROUPS,
  buildFooterGroups,
  isLiveRoute,
  liveMetadata,
  siteRoutes,
  type SiteLink,
} from "./sitemap";

const nav = enSiteCopy.site.nav;
const preview = () => true;

const landingRoutes = resolve(__dirname, "../../../app/(landing)");

const internalLinks = (links: SiteLink[]) => links.filter((link) => !link.external);
const navLinks = (isLive?: (href: string) => boolean) =>
  buildFooterGroups("/docs", nav, isLive).flatMap((group) => group.links);

describe("site map", () => {
  it("points every internal footer link at a served route", () => {
    const routes = new Set(siteRoutes());
    for (const link of internalLinks(navLinks(preview))) {
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

  it("links only released pages outside preview builds", () => {
    for (const link of internalLinks(navLinks())) {
      expect(isLiveRoute(link.href), `${link.label} → ${link.href}`).toBe(true);
    }
    expect(buildFooterGroups("/docs", nav).map((group) => group.title)).toEqual([nav.resources, nav.company, nav.trust]);
  });

  it("drops a footer column once none of its pages are released", () => {
    const onlyDesktop = (href: string) => href === "/product/desktop";
    const product = buildFooterGroups("/docs", nav, onlyDesktop).find((group) => group.title === nav.product);
    expect(product?.links.map((link) => link.href)).toEqual(["/product/desktop"]);
    expect(buildFooterGroups("/docs", nav, () => false).find((group) => group.title === nav.product)).toBeUndefined();
  });

  it("takes withdrawn pages out of the footer while keeping their routes", () => {
    expect(isLiveRoute("/usecases")).toBe(false);
    expect(internalLinks(navLinks()).map((link) => link.href)).not.toContain("/usecases");
    expect(siteRoutes()).toContain("/usecases");
  });

  it("withholds the metadata of unreleased pages", () => {
    const metadata = { title: "Pricing" };
    expect(liveMetadata("/pricing", metadata)).toEqual({});
    expect(liveMetadata("/about", metadata)).toBe(metadata);
  });

  it.each(["/docs", "/docs/zh", "/docs/ja"])("links documentation to the %s docs", (docsHref) => {
    const footerDocs = buildFooterGroups(docsHref, nav)
      .flatMap((group) => group.links)
      .find((link) => link.label === "Documentation");
    expect(footerDocs?.href).toBe(docsHref);
  });
});
