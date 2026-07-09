"use client";

import { Fragment, type ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { PageHeader } from "./page-header";
import { AppLink } from "../navigation";

/**
 * One ancestor crumb. Always a clickable link to the segment's container — the
 * breadcrumb expresses a containment chain, so every segment must navigate
 * somewhere. Non-navigable chrome (skeletons, "unknown" states) does NOT belong
 * here; omit the segment instead.
 */
export interface BreadcrumbSegment {
  href: string;
  /** Plain text, or a composed node (e.g. icon + label). */
  label: ReactNode;
  /**
   * Overrides the default `shrink-0`. Pass `flex items-center gap-1 min-w-0
   * max-w-72` for a truncating segment (e.g. a long project title).
   */
  className?: string;
}

interface BreadcrumbHeaderProps {
  /** Ancestor links, rendered left-to-right with chevron separators. */
  segments: BreadcrumbSegment[];
  /** The current page — non-clickable leaf. Caller controls styling/adornments. */
  leaf: ReactNode;
  /** Right-side actions. Wrapped in a `shrink-0` flex row; omit for none. */
  actions?: ReactNode;
  className?: string;
}

/**
 * Unified detail-page header: `{ancestor › ancestor › …} leaf  [actions]`.
 *
 * Replaces the per-page hand-rolled breadcrumbs that had drifted into four
 * different styles (workspace-name root, `/` separator, back-arrow, raw div).
 * The mental model is identical everywhere: the leading crumbs are the thing's
 * real containers and clicking one navigates up to it.
 */
export function BreadcrumbHeader({ segments, leaf, actions, className }: BreadcrumbHeaderProps) {
  return (
    <PageHeader className={cn("gap-2 bg-background text-sm", className)}>
      <div className="flex flex-1 items-center gap-1.5 min-w-0">
        {segments.map((segment) => (
          <Fragment key={segment.href}>
            <AppLink
              href={segment.href}
              className={cn(
                "text-muted-foreground hover:text-foreground transition-colors",
                segment.className ?? "shrink-0",
              )}
            >
              {segment.label}
            </AppLink>
            <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
          </Fragment>
        ))}
        {leaf}
      </div>
      {actions ? <div className="flex items-center gap-1 shrink-0">{actions}</div> : null}
    </PageHeader>
  );
}
