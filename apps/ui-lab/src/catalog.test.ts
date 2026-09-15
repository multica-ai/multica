// @vitest-environment node
import { describe, expect, it } from "vitest";
import { catalog, pageHref, resolveCatalogRoute } from "./catalog";
import { scenes } from "./protocol";

describe("design system catalog routes", () => {
  it("opens the overview for the root and rejects unknown or incomplete paths", () => {
    for (const hash of ["", "#", "#/", "#/overview"]) {
      expect(resolveCatalogRoute(hash)).toEqual({ kind: "overview" });
    }
    for (const hash of [
      "#/missing",
      "#/foundations/button",
      "#/components/button/extra",
      "#components",
      "#/components/",
    ]) {
      expect(resolveCatalogRoute(hash)).toEqual({ kind: "notFound" });
    }
  });
  it("gives each module and page a unique, reloadable location", () => {
    const paths = new Set<string>();
    for (const module of catalog) {
      expect(resolveCatalogRoute(pageHref(module.id))).toEqual({
        kind: "module",
        module,
      });
      for (const page of module.pages) {
        const path = pageHref(module.id, page.id);
        expect(paths.has(path)).toBe(false);
        paths.add(path);
        expect(resolveCatalogRoute(path)).toEqual({
          kind: "page",
          module,
          page,
        });
      }
    }
  });
  it("keeps every production preview reachable and marks unimplemented source operations as planned", () => {
    const previews = catalog
      .flatMap((module) => module.pages)
      .flatMap((page) =>
        page.content.kind === "preview" ? [page.content.scene] : [],
      );
    expect(previews.sort()).toEqual(scenes.map((scene) => scene.id).sort());
    for (const page of ["impact", "source"] as const) {
      const route = resolveCatalogRoute(pageHref("changes", page));
      expect(route.kind === "page" && route.page.content.kind).toBe("planned");
    }
  });
});
