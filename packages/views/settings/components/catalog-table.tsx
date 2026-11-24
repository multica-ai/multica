"use client";

import type { CSSProperties, ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";

// The table shared by the settings catalogs of small named things: Labels,
// Properties and Quick Actions. They read as one surface, so the header, the
// row grid and the narrow fallback live here instead of being copied per tab.
//
// The breakpoint is the card's own width (`@3xl/catalog`, 48rem), not the
// viewport: the settings column is much narrower than the window on tablets
// and in narrow desktop windows. Below it a row is the name with the `…` menu
// on the right, over one wrapping line of metadata. Above it the metadata
// wrapper is `display: contents`, so each field drops into its own column.
//
// Cells that need a different size or visibility per layout use the same
// `@3xl/catalog:` variant. The metadata wrapper is `text-caption` when narrow
// and `text-body` when wide, and `contents` still passes that down.

/** Track for the trailing `…` menu column, appended to every catalog. */
const MENU_TRACK = "2rem";

export function CatalogTable({
  columns,
  children,
}: {
  /** CSS `grid-template-columns` tracks for the wide layout, menu column excluded. */
  columns: string;
  children: ReactNode;
}) {
  return (
    <div
      className="@container/catalog overflow-hidden rounded-lg border border-surface-border bg-card"
      style={{ "--catalog-columns": `${columns} ${MENU_TRACK}` } as CSSProperties}
    >
      {children}
    </div>
  );
}

/** Column titles, shown only in the wide layout. */
export function CatalogHeader({ children }: { children: ReactNode }) {
  return (
    <div className="hidden grid-cols-(--catalog-columns) gap-4 border-b border-surface-border bg-muted/20 px-4 py-2.5 text-caption font-medium text-muted-foreground @3xl/catalog:grid">
      {children}
      <span />
    </div>
  );
}

export function CatalogRows({ children }: { children: ReactNode }) {
  return <div className="divide-y divide-surface-border">{children}</div>;
}

export function CatalogRow({
  name,
  meta,
  menu,
  className,
}: {
  /** First column; also the first line when narrow. */
  name: ReactNode;
  /** One element per remaining column, in column order. */
  meta: ReactNode;
  menu?: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1.5 px-4 py-3 @3xl/catalog:grid-cols-(--catalog-columns) @3xl/catalog:gap-4",
        className,
      )}
    >
      {name}
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground @3xl/catalog:contents @3xl/catalog:text-body">
        {meta}
      </div>
      <div className="col-[2] row-[1/span_2] @3xl/catalog:col-auto @3xl/catalog:row-auto">
        {menu}
      </div>
    </div>
  );
}
