import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ArrowRight, CircleDashed, RotateCcw } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  catalog,
  availableCount,
  pageHref,
  type CatalogRoute,
} from "./catalog";
import { moduleIcons } from "./catalog-navigation";
import { TokenChanges } from "./token-changes";
import { changeCount, emptyDraft, type Draft } from "./tokens";

export function CatalogContent({
  route,
  draft,
  onEdit,
  onSave,
  onExport,
  savedDesigns,
  exportContent,
}: {
  route: CatalogRoute;
  draft: Draft;
  onEdit: (draft: Draft) => void;
  onSave: () => void;
  onExport: () => void;
  savedDesigns: ReactNode;
  exportContent: ReactNode;
}) {
  const { t } = useTranslation("uiLab");
  const status = (available: boolean) => (
    <span className={`catalog-status ${available ? "available" : ""}`}>
      <span className="catalog-dot" />
      {t(($) => $.system.status[available ? "available" : "planned"])}
    </span>
  );
  return (
    <div
      className="catalog-content"
      key={
        route.kind === "page"
          ? `${route.module.id}/${route.page.id}`
          : route.kind === "module"
            ? route.module.id
            : route.kind
      }
    >
      {route.kind === "overview" && (
        <div className="catalog-module-grid">
          {catalog.map((module, index) => {
            const Icon = moduleIcons[module.id];
            return (
              <a
                className="catalog-module-card"
                href={pageHref(module.id)}
                key={module.id}
              >
                <div className="catalog-card-top">
                  <Icon />
                  <span>{String(index + 1).padStart(2, "0")}</span>
                </div>
                <h2>{t(($) => $.system.modules[module.id])}</h2>
                <p>{t(($) => $.system.descriptions[module.id])}</p>
                <div className="catalog-card-footer">
                  <span>
                    {t(($) => $.system.status.progress, {
                      ready: availableCount(module),
                      total: module.pages.length,
                    })}
                  </span>
                  <ArrowRight />
                </div>
              </a>
            );
          })}
        </div>
      )}
      {route.kind === "module" && (
        <>
          <div className="catalog-module-summary">
            <p>{t(($) => $.system.descriptions[route.module.id])}</p>
            <span>
              {t(($) => $.system.status.progress, {
                ready: availableCount(route.module),
                total: route.module.pages.length,
              })}
            </span>
          </div>
          <div className="catalog-page-grid">
            {route.module.pages.map((page) => (
              <a
                className="catalog-page-card"
                href={pageHref(route.module.id, page.id)}
                key={page.id}
              >
                <h2>{t(($) => $.system.pages[page.id])}</h2>
                {status(page.content.kind !== "planned")}
                <ArrowRight />
              </a>
            ))}
          </div>
        </>
      )}
      {route.kind === "page" && route.page.content.kind === "planned" && (
        <div className="catalog-placeholder">
          <CircleDashed className="catalog-placeholder-icon" />
          {status(false)}
          <p>{t(($) => $.system.empty.message)}</p>
          <a href={pageHref(route.module.id)}>
            {t(($) => $.system.empty.back, {
              module: t(($) => $.system.modules[route.module.id]),
            })}
            <ArrowRight />
          </a>
        </div>
      )}
      {route.kind === "notFound" && (
        <div className="catalog-placeholder">
          <CircleDashed className="catalog-placeholder-icon" />
          <a href="#/overview">
            {t(($) => $.system.navigation.overview)}
            <ArrowRight />
          </a>
        </div>
      )}
      {route.kind === "page" && route.page.content.kind === "changes" && (
        <div className="catalog-changes">
          {route.page.content.view === "review" && (
            <>
              <div className="catalog-changes-actions">
                <a href={pageHref("components", "button")}>
                  {t(($) => $.system.changes.return)}
                  <ArrowRight />
                </a>
                <Button
                  variant="ghost"
                  disabled={!changeCount(draft)}
                  onClick={() => onEdit(emptyDraft())}
                >
                  <RotateCcw />
                  {t(($) => $.system.changes.reset)}
                </Button>
                <Button variant="outline" onClick={onExport}>
                  {t(($) => $.lab.actions.export)}
                </Button>
              </div>
              <TokenChanges draft={draft} onEdit={onEdit} />
            </>
          )}
          {route.page.content.view === "saved" && (
            <>
              <div className="catalog-changes-actions">
                <Button onClick={onSave}>{t(($) => $.lab.actions.save)}</Button>
              </div>
              {savedDesigns}
            </>
          )}
          {route.page.content.view === "export" && exportContent}
        </div>
      )}
    </div>
  );
}
