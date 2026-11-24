"use client";

import type { ReactNode } from "react";
import { useReducedMotion } from "@multica/core/appearance";
import { ChevronRight } from "@multica/ui/icons";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { Collapsible, CollapsibleContent } from "@multica/ui/components/ui/collapsible";
import { cn } from "@multica/ui/lib/utils";

/**
 * Shared disclosure header; actions stay outside the disclosure button.
 *
 * Inset like a `PropRow` (`px-2` around an 8px hit-area padding) so the title
 * starts on the same edge as the labels and rows of every section body under
 * it. The padding is 7px because `Button` adds a 1px transparent border. The
 * right inset puts the glyph of an `icon-xs` action, not its hit area, on the
 * edge where row content ends.
 *
 * An open section lists what it counts, so the count only shows while it is
 * collapsed, unless `countWhenOpen` says the open list is cut short.
 */
export function IssueSidebarSectionHeader({
  title, open, onOpenChange, count, countWhenOpen = false, actions, status, id, collapsible = true,
}: {
  title: ReactNode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  collapsible?: boolean;
  count?: number;
  countWhenOpen?: boolean;
  actions?: ReactNode;
  status?: ReactNode;
  id?: string;
}) {
  const showCount = count !== undefined && count > 0 && (!open || countWhenOpen);
  return (
    <div data-slot="issue-sidebar-section-header" className="mb-1 flex min-w-0 items-center gap-1 pr-2.5 pl-2">
      {collapsible ? <Button
        id={id}
        type="button"
        variant="ghost"
        size="sm"
        aria-expanded={open}
        onClick={() => onOpenChange(!open)}
        className={cn("min-w-0 flex-1 shrink justify-start px-[7px] font-medium aria-expanded:bg-transparent active:not-aria-[haspopup]:translate-y-0", !open && "text-muted-foreground")}
      >
        <span className="truncate">{title}</span>
        <ChevronRight aria-hidden="true" className={cn("!size-3 shrink-0 text-faint-foreground transition-transform duration-150 ease-[cubic-bezier(0.23,1,0.32,1)] motion-reduce:transition-none", open && "rotate-90")} />
        {showCount && (
          <>
            {" "}
            <span data-slot="section-count" className="shrink-0 rounded-xs bg-muted px-1 text-micro font-medium tabular-nums text-muted-foreground">{count}</span>
          </>
        )}
      </Button> : (
        <div id={id} className={cn(buttonVariants({ variant: "ghost", size: "sm" }), "min-w-0 flex-1 shrink justify-start px-[7px] font-medium hover:bg-transparent dark:hover:bg-transparent")}>
          <span className="truncate">{title}</span>
        </div>
      )}
      {status}
      {actions && <div className="flex shrink-0 items-center gap-1">{actions}</div>}
    </div>
  );
}

/** Shared smooth height + opacity collapse for issue sidebar & detail sections. */
export function IssueSidebarCollapsible({
  open,
  children,
  className,
}: {
  open: boolean;
  children: ReactNode;
  className?: string;
}) {
  const reduceMotion = useReducedMotion();

  return (
    <Collapsible open={open}>
      <CollapsibleContent className={cn(reduceMotion && "transition-none", className)}>
        {children}
      </CollapsibleContent>
    </Collapsible>
  );
}

export function IssueSidebarStack({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("flex flex-col gap-4 [overflow-anchor:none] [&>*]:flow-root", className)}>{children}</div>;
}

/**
 * Label column sized by `sizerLabels` rather than by the rows present, so
 * values start on the same edge on every issue. Longer labels wrap.
 */
export function IssueSidebarPropertyGrid({ children, sizerLabels }: { children: ReactNode; sizerLabels?: readonly string[] }) {
  const sized = !!sizerLabels?.length;
  return (
    // The sizer adds an empty last row; the negative margin takes back its gap.
    <div className={cn("grid grid-cols-[fit-content(min(44%,11rem))_minmax(0,1fr)] gap-x-2 gap-y-1 px-4", sized && "-mb-1")}>
      {children}
      {sized && (
        <div aria-hidden="true" className="invisible col-start-1 h-0 overflow-hidden text-label">
          {/* Generated content keeps the labels out of the page's text. */}
          {sizerLabels.map((label) => <div key={label} data-label={label} className="whitespace-nowrap before:content-[attr(data-label)]" />)}
        </div>
      )}
    </div>
  );
}
