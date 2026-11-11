"use client";

import type { ReactNode } from "react";
import { ChevronRight } from "@multica/ui/icons";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";

/**
 * Shared disclosure header; actions stay outside the disclosure button.
 *
 * Inset like a `PropRow` (`px-2` around an 8px hit-area padding) so the title
 * starts on the same edge as the labels and rows of every section body under
 * it. The padding is 7px because `Button` adds a 1px transparent border.
 */
export function IssueSidebarSectionHeader({
  title, open, onOpenChange, count, actions, status, id, collapsible = true,
}: {
  title: ReactNode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  collapsible?: boolean;
  count?: number;
  actions?: ReactNode;
  status?: ReactNode;
  id?: string;
}) {
  return (
    <div data-slot="issue-sidebar-section-header" className="mb-1 flex min-w-0 items-center gap-1 px-2">
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
        <ChevronRight aria-hidden="true" className={cn("!size-3 shrink-0 text-faint-foreground transition-transform motion-reduce:transition-none", open && "rotate-90")} />
        {count !== undefined && count > 0 && (
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

/** Shared spacing separates rendered sections without adding divider lines. */
export function IssueSidebarStack({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn("flex flex-col gap-6 [overflow-anchor:none] [&>*]:flow-root", className)}>{children}</div>;
}

/** Fits labels to their content while reserving space for values and the hover gutter. */
export function IssueSidebarPropertyGrid({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-[fit-content(min(44%,11rem))_minmax(0,1fr)] gap-x-2 gap-y-1 px-4">{children}</div>;
}
