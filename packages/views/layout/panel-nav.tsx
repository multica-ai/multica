"use client";

import { useId, useRef } from "react";
import { ChevronDown } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { AppLink } from "../navigation";

/**
 * Building blocks of the space panels. Rows share the Settings navigation
 * styling: Settings renders its own column in the same card, and switching
 * between it and a shell panel must not change how a nav row looks.
 */

/** A section root stays lit on its child routes (a project on its detail page). */
export function isPathWithin(pathname: string, href: string): boolean {
  return pathname === href || pathname.startsWith(href + "/");
}

/**
 * The title band of every left column — the space panels and the list columns
 * of Inbox, Chat and Settings — so the space name reads the same in all of
 * them. Pages that own the column (Inbox, Chat, Settings) render it as their h1.
 */
export function PanelHeader({
  title,
  as: Heading = "h2",
  leading,
  meta,
  children,
}: {
  title: React.ReactNode;
  as?: "h1" | "h2";
  /** Before the title: the nav trigger on pages that also render below `md`. */
  leading?: React.ReactNode;
  /** Beside the title, e.g. a count. */
  meta?: React.ReactNode;
  /** Trailing actions (icon buttons). */
  children?: React.ReactNode;
}) {
  return (
    <div className="flex h-12 shrink-0 items-center gap-1 pl-5 pr-2">
      {leading}
      <div className="flex min-w-0 flex-1 items-center gap-2">
        {/* A control passed as the title truncates its own label; clipping
            here would also cut off its hover background. */}
        <Heading
          className={cn(
            "text-title-sm font-semibold",
            typeof title === "string" ? "truncate" : "flex min-w-0",
          )}
        >
          {title}
        </Heading>
        {meta}
      </div>
      {children}
    </div>
  );
}

export function PanelBody({
  children,
  onClickCapture,
}: {
  children: React.ReactNode;
  onClickCapture?: React.MouseEventHandler<HTMLDivElement>;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(ref, 24);
  return (
    <div
      ref={ref}
      style={fadeStyle}
      onClickCapture={onClickCapture}
      className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-4 [&>section:first-child]:mt-0"
    >
      {children}
    </div>
  );
}

export function PanelSection({
  label,
  action,
  collapsed,
  onCollapsedChange,
  children,
}: {
  label: string;
  /** Rendered at the end of the label row (e.g. a create button). */
  action?: React.ReactNode;
  /** With `onCollapsedChange`, the label toggles the section's content. */
  collapsed?: boolean;
  onCollapsedChange?: (collapsed: boolean) => void;
  children: React.ReactNode;
}) {
  const id = useId();
  const contentId = useId();
  const collapsible = !!onCollapsedChange;
  return (
    <section aria-labelledby={id} className="mt-5">
      <div className="group/section flex h-7 items-center gap-1 pl-3 pr-1">
        <h3 id={id} className="min-w-0 flex-1 truncate text-caption font-medium text-muted-foreground">
          {collapsible ? (
            <button
              type="button"
              aria-expanded={!collapsed}
              aria-controls={collapsed ? undefined : contentId}
              onClick={() => onCollapsedChange(!collapsed)}
              className="-mx-1 flex max-w-full items-center gap-0.5 rounded-md px-1 outline-none hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring"
            >
              <span className="truncate">{label}</span>
              <ChevronDown
                aria-hidden
                className={cn(
                  "size-3.5 shrink-0 transition-[transform,opacity] duration-150 ease-out motion-reduce:transition-none",
                  collapsed
                    ? "-rotate-90"
                    : "opacity-0 group-hover/section:opacity-100 group-focus-within/section:opacity-100",
                )}
              />
            </button>
          ) : (
            label
          )}
        </h3>
        {action}
      </div>
      {!collapsed && <div id={contentId}>{children}</div>}
    </section>
  );
}

export function PanelList({
  children,
  className,
  ...props
}: React.ComponentProps<"ul">) {
  return (
    <ul className={cn("space-y-px", className)} {...props}>
      {children}
    </ul>
  );
}

export const PANEL_ROW_CLASS =
  "flex min-h-8 w-full items-center gap-2.5 rounded-lg px-3 py-1.5 text-left text-body transition-colors outline-none focus-visible:outline-2 focus-visible:outline-ring";

export function panelRowStateClass(active: boolean) {
  return active
    ? "bg-surface-selected text-surface-selected-foreground hover:bg-surface-selected"
    : "text-muted-foreground hover:bg-surface-hover hover:text-foreground";
}

export function PanelCount({ value, className }: { value: number; className?: string }) {
  if (value <= 0) return null;
  return (
    <span className={cn("shrink-0 text-caption tabular-nums text-faint-foreground", className)}>
      {value}
    </span>
  );
}

interface PanelLinkProps {
  href: string;
  label: string;
  active: boolean;
  /** Leading visual; icons are sized by the row. */
  icon?: React.ReactNode;
  /** Second line under the label (agent activity, schedule). */
  description?: React.ReactNode;
  /** End of the row: a count, a status, a tag. */
  trailing?: React.ReactNode;
  /** Runs before navigation, e.g. to select a list scope the page reads. */
  onNavigate?: () => void;
  className?: string;
}

export function PanelLink({
  href,
  label,
  active,
  icon,
  description,
  trailing,
  onNavigate,
  className,
}: PanelLinkProps) {
  return (
    <li>
      <AppLink
        href={href}
        newTabTitle={label}
        aria-current={active ? "page" : undefined}
        onClick={onNavigate}
        className={cn(
          PANEL_ROW_CLASS,
          panelRowStateClass(active),
          "[&_svg:not([class*='size-'])]:size-4 [&_svg]:shrink-0",
          className,
        )}
      >
        {icon}
        {description ? (
          <span className="flex min-w-0 flex-1 flex-col">
            <span className="truncate">{label}</span>
            <span className="truncate text-caption font-normal text-muted-foreground">
              {description}
            </span>
          </span>
        ) : (
          <span className="min-w-0 flex-1 truncate">{label}</span>
        )}
        {trailing}
      </AppLink>
    </li>
  );
}
