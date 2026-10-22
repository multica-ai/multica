import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Blocks,
  BookOpen,
  Box,
  ChevronDown,
  Component,
  History,
  LayoutDashboard,
  Palette,
} from "lucide-react";
import {
  catalog,
  availableCount,
  pageHref,
  type ModuleId,
  type CatalogRoute,
} from "./catalog";

export const moduleIcons = {
  foundations: Palette,
  components: Component,
  patterns: Blocks,
  product: Box,
  layouts: LayoutDashboard,
  changes: History,
};

export function CatalogNavigation({
  route,
  onNavigate,
}: {
  route: CatalogRoute;
  onNavigate: () => void;
}) {
  const { t } = useTranslation("uiLab");
  const [expanded, setExpanded] = useState<Partial<Record<ModuleId, boolean>>>({
    components: true,
  });
  const currentModule = "module" in route ? route.module.id : undefined;
  const currentPage = route.kind === "page" ? route.page.id : undefined;
  useEffect(() => {
    if (currentModule)
      setExpanded((previous) => ({ ...previous, [currentModule]: true }));
  }, [currentModule, currentPage]);
  return (
    <nav
      className="catalog-navigation"
      aria-label={t(($) => $.system.navigation.title)}
    >
      <a
        className="catalog-home"
        href="#/overview"
        aria-current={route.kind === "overview" ? "page" : undefined}
        onClick={onNavigate}
      >
        <BookOpen />
        <span>{t(($) => $.system.navigation.overview)}</span>
      </a>
      {catalog.map((module) => {
        const Icon = moduleIcons[module.id];
        const open = expanded[module.id] ?? currentModule === module.id;
        return (
          <section className="catalog-nav-group" key={module.id}>
            <div className="catalog-nav-heading">
              <a
                href={pageHref(module.id)}
                aria-current={
                  route.kind === "module" && currentModule === module.id
                    ? "page"
                    : undefined
                }
                onClick={onNavigate}
              >
                <Icon />
                <span>{t(($) => $.system.modules[module.id])}</span>
              </a>
              <button
                type="button"
                aria-label={t(($) => $.system.navigation.expand, {
                  module: t(($) => $.system.modules[module.id]),
                })}
                aria-expanded={open}
                aria-controls={`nav-${module.id}`}
                onClick={() =>
                  setExpanded((previous) => ({
                    ...previous,
                    [module.id]: !open,
                  }))
                }
              >
                <span>
                  {availableCount(module)}/{module.pages.length}
                </span>
                <ChevronDown className={open ? "" : "is-collapsed"} />
              </button>
            </div>
            <div
              className="catalog-nav-items"
              id={`nav-${module.id}`}
              hidden={!open}
            >
              {module.pages.map((page) => (
                <a
                  key={page.id}
                  href={pageHref(module.id, page.id)}
                  aria-current={
                    route.kind === "page" &&
                    currentModule === module.id &&
                    route.page.id === page.id
                      ? "page"
                      : undefined
                  }
                  onClick={onNavigate}
                >
                  <span>{t(($) => $.system.pages[page.id])}</span>
                  <span
                    className={`catalog-dot ${page.content.kind === "planned" ? "" : "available"}`}
                    role="img"
                    aria-label={t(
                      ($) =>
                        $.system.status[
                          page.content.kind === "planned"
                            ? "planned"
                            : "available"
                        ],
                    )}
                  />
                </a>
              ))}
            </div>
          </section>
        );
      })}
    </nav>
  );
}
