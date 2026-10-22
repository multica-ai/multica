import type en from "./locales/en.json";
import type { Scene } from "./protocol";

export type ModuleId = keyof typeof en.system.modules;
export type PageId = keyof typeof en.system.pages;
export type CatalogPage = {
  id: PageId;
  content:
    | { kind: "planned" }
    | { kind: "preview"; scene: Scene }
    | { kind: "changes"; view: "review" | "saved" | "export" };
};
export type CatalogModule = { id: ModuleId; pages: readonly CatalogPage[] };
const planned = (...ids: PageId[]): CatalogPage[] =>
  ids.map((id) => ({ id, content: { kind: "planned" } }));

// One registry drives navigation, module indexes, availability and routing.
export const catalog: readonly CatalogModule[] = [
  {
    id: "foundations",
    pages: [
      { id: "colors", content: { kind: "preview", scene: "colors" } },
      ...planned("typography", "spacing", "radius", "shadows", "icons"),
      { id: "motion", content: { kind: "preview", scene: "motion" } },
    ],
  },
  {
    id: "components",
    pages: [
      { id: "gallery", content: { kind: "preview", scene: "components" } },
      { id: "button", content: { kind: "preview", scene: "button" } },
      { id: "dialog", content: { kind: "preview", scene: "dialog" } },
      ...planned(
        "input",
        "select",
        "checkbox",
        "switch",
        "badge",
        "tabs",
        "tooltip",
        "popover",
      ),
    ],
  },
  {
    id: "patterns",
    pages: planned(
      "forms",
      "filters",
      "properties",
      "confirmation",
      "feedback",
    ),
  },
  {
    id: "product",
    pages: planned(
      "issueRow",
      "statusPicker",
      "assigneePicker",
      "comments",
      "agentRun",
    ),
  },
  {
    id: "layouts",
    pages: [
      ...planned("sidebar"),
      { id: "list", content: { kind: "preview", scene: "list" } },
      { id: "detail", content: { kind: "preview", scene: "detail" } },
      ...planned("settings", "split"),
    ],
  },
  {
    id: "changes",
    pages: [
      { id: "review", content: { kind: "changes", view: "review" } },
      { id: "saved", content: { kind: "changes", view: "saved" } },
      { id: "export", content: { kind: "changes", view: "export" } },
      ...planned("impact", "source"),
    ],
  },
];
export const pageHref = (module: ModuleId, page?: PageId) =>
  `#/${module}${page ? `/${page}` : ""}`;
export const availableCount = (module: CatalogModule) =>
  module.pages.filter((page) => page.content.kind !== "planned").length;
export type CatalogRoute =
  | { kind: "overview" }
  | { kind: "module"; module: CatalogModule }
  | { kind: "page"; module: CatalogModule; page: CatalogPage }
  | { kind: "notFound" };

export function resolveCatalogRoute(hash: string): CatalogRoute {
  if (!hash || hash === "#" || hash === "#/" || hash === "#/overview")
    return { kind: "overview" };
  const match = /^#\/([^/]+)(?:\/([^/]+))?$/.exec(hash);
  const module = catalog.find((item) => item.id === match?.[1]);
  if (!module) return { kind: "notFound" };
  if (!match?.[2]) return { kind: "module", module };
  const page = module.pages.find((item) => item.id === match[2]);
  return page ? { kind: "page", module, page } : { kind: "notFound" };
}
