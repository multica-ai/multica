"use client";

import { cloneElement } from "react";
import { ArrowDown, ArrowUp } from "lucide-react";

import { cn } from "../../lib/utils";

// Linear-style list grid. The container declares every column track once via
// a literal `grid-cols-[...]` class (plus responsive variants); the header and
// each row span the full template with `grid-cols-subgrid`, so column widths
// have a single source of truth and never drift between header, rows, and
// skeletons.
//
// Conventions the container class must follow:
// - First and last tracks are edge-padding columns (e.g. 1.25rem) so row
//   hover backgrounds stay full-bleed while content aligns with page chrome.
//   ListGridHeader/ListGridRow render the matching placeholder cells.
// - Responsiveness is TWO-ZONE and CONTAINER-query driven (wrap the ListGrid
//   in a `@container` element; `@<bp>:` variants, never viewport `sm:`/`lg:`,
//   so sidebars and split panes are accounted for):
//   - ≥ @2xl: WYSIWYG — every user-enabled column renders. The grid carries
//     `@2xl:min-w-[var(--…-minw)]` (Σ enabled tracks + gaps, computed from
//     the page's column-width constants) and the wrapper has
//     `overflow-x-auto`, so an over-provisioned column set scrolls instead
//     of clipping. An enabled column must NEVER be silently hidden behind a
//     width tier — that "dead toggle" bug shipped twice.
//   - < @2xl: a static core template (name + one key column), no horizontal
//     scroll, column toggles don't apply. Non-core cells carry
//     `hidden @2xl:flex`; display:none cells drop out of subgrid
//     auto-placement so the remaining cells fill the right tracks.
// - Keep the class a literal string in the page source so Tailwind sees it.

export type ListGridSortDirection = "asc" | "desc";

function ListGrid({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      role="table"
      className={cn("grid w-full min-w-0 content-start gap-x-3", className)}
      {...props}
    />
  );
}

function ListGridHeader({
  className,
  children,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      role="row"
      className={cn(
        "group/header sticky top-0 z-10 col-span-full grid h-9 grid-cols-subgrid items-center bg-background after:pointer-events-none after:absolute after:inset-x-0 after:top-full after:h-3 after:bg-gradient-to-b after:from-background after:to-transparent",
        className,
      )}
      {...props}
    >
      <span aria-hidden="true" />
      {children}
      <span aria-hidden="true" />
    </div>
  );
}

interface ListGridHeaderCellProps
  extends React.HTMLAttributes<HTMLDivElement> {
  /** Current sort state of this column; `false` when not the active sort. */
  sorted?: ListGridSortDirection | false;
  /** When provided the header renders as a sort button. */
  onSort?: () => void;
  align?: "left" | "right";
}

function ListGridHeaderCell({
  sorted = false,
  onSort,
  align = "left",
  className,
  children,
  ...props
}: ListGridHeaderCellProps) {
  if (!onSort) {
    return (
      <div
        className={cn(
          "flex min-w-0 items-center px-2 text-xs text-muted-foreground",
          align === "right" && "justify-end",
          className,
        )}
        {...props}
      >
        {children}
      </div>
    );
  }
  const Arrow = sorted === "asc" ? ArrowUp : ArrowDown;
  return (
    <div
      className={cn(
        "flex min-w-0 items-center px-2",
        align === "right" && "justify-end",
        className,
      )}
      {...props}
    >
      <button
        type="button"
        onClick={onSort}
        className={cn(
          "group/sort flex h-6 items-center gap-0.5 rounded-md text-xs transition-colors",
          // Active sort column: emphasis via weight + full foreground color
          // only — no background, so the header row stays quiet.
          sorted
            ? "font-medium text-foreground"
            : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
          align === "right" ? "-mr-1.5 flex-row-reverse pl-1 pr-1.5" : "-ml-1.5 pl-1.5 pr-1",
        )}
      >
        {children}
        <Arrow
          className={cn(
            "size-3 shrink-0",
            sorted ? "opacity-100" : "opacity-0 group-hover/sort:opacity-50",
          )}
        />
      </button>
    </div>
  );
}

// Rows area — a plain subgrid passthrough that groups the rows and carries
// the virtualization padding. It does NOT scroll: both scroll axes live on
// the single outer wrapper (`overflow-auto @container`), with the sticky
// header pinning inside that scroller. Splitting horizontal scrolling
// (wrapper) from vertical scrolling (this element) connected by an h-full
// percentage bridge caused a non-converging layout loop (flickering double
// scrollbars), clipped the last row under the horizontal scrollbar, and
// let a classic vertical scrollbar shift row tracks relative to the header.
function ListGridBody({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      className={cn(
        "col-span-full grid grid-cols-subgrid content-start",
        className,
      )}
      {...props}
    />
  );
}

// Bottom clearance appended to the rows area's padding so the last row can
// scroll clear of floating UI anchored to the pane's bottom edge (the chat
// FAB at bottom-right covers ~48px; the batch toolbar ~62px). One row of
// extra runway keeps both off the final row's kebab.
export const LIST_GRID_BOTTOM_CLEARANCE = 64;

interface ListGridRowProps extends React.HTMLAttributes<HTMLElement> {
  /**
   * Base UI-style render prop: pass an element (e.g. `<AppLink href={...} />`)
   * to use it as the row root; the row classes and cells are merged onto it.
   */
  render?: React.ReactElement<{
    className?: string;
    children?: React.ReactNode;
  }>;
}

function ListGridRow({ render, className, children, ...props }: ListGridRowProps) {
  const rowClassName = cn(
    "group/row col-span-full grid h-12 grid-cols-subgrid items-center transition-colors hover:bg-accent/40",
    className,
  );
  const content = (
    <>
      <span aria-hidden="true" />
      {children}
      <span aria-hidden="true" />
    </>
  );
  if (render) {
    return cloneElement(
      render,
      { ...props, className: cn(rowClassName, render.props.className) },
      content,
    );
  }
  return (
    <div className={rowClassName} {...props}>
      {content}
    </div>
  );
}

// Cells and header cells carry the same default horizontal padding so the
// header can never drift out of alignment with row content. Structural
// columns (checkbox, kebab) opt out with `px-0`.
function ListGridCell({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("flex min-w-0 items-center px-2", className)}
      {...props}
    />
  );
}

export {
  ListGrid,
  ListGridBody,
  ListGridHeader,
  ListGridHeaderCell,
  ListGridRow,
  ListGridCell,
};
