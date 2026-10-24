import type { ReactNode } from "react";
import { Bot, ChevronRight, Cog, Puzzle } from "lucide-react";
import { WORKSPACE_PAGES } from "@multica/core/paths";
import { ROUTE_ICON_COMPONENTS } from "@multica/views/layout";
import { Avatar, AvatarFallback } from "@multica/ui/components/ui/avatar";
import { cn } from "@multica/ui/lib/utils";
import type { ActorType } from "./fixtures";

// The same rail/gutter constants the production detail pages read
// (packages/views/layout/page-header.tsx). Repeated rather than imported
// because the layout package does not export them.
export const PAGE_GUTTER = "px-4";
export const PAGE_RAIL = "mx-auto w-full max-w-[1440px]";

export const SkillIcon = ROUTE_ICON_COMPONENTS[WORKSPACE_PAGES.skills.icon];

/** Mirrors BreadcrumbHeader: 48px, bottom border, muted crumbs, actions right. */
export function Breadcrumbs({
  crumbs,
  leaf,
  actions,
  onNavigate,
}: {
  crumbs: { label: string; href: string }[];
  leaf: ReactNode;
  actions?: ReactNode;
  onNavigate: (href: string) => void;
}) {
  return (
    <header
      className={cn(
        "flex h-12 shrink-0 items-center gap-2 border-b bg-background text-body",
        PAGE_GUTTER,
      )}
    >
      <div className="flex min-w-0 flex-1 items-center gap-1.5">
        {crumbs.map((crumb) => (
          <span key={crumb.href} className="contents">
            <button
              type="button"
              onClick={() => onNavigate(crumb.href)}
              className="shrink-0 text-muted-foreground transition-colors hover:text-foreground"
            >
              {crumb.label}
            </button>
            <ChevronRight className="h-3 w-3 shrink-0 text-faint-foreground" />
          </span>
        ))}
        {leaf}
      </div>
      {actions ? (
        <div className="flex shrink-0 items-center gap-1">{actions}</div>
      ) : null}
    </header>
  );
}

/** Mirrors the skill/agent detail tab bar. */
export function DetailTabs<T extends string>({
  tabs,
  active,
  onSelect,
  label,
}: {
  tabs: { id: T; label: string }[];
  active: T;
  onSelect: (id: T) => void;
  label: string;
}) {
  return (
    <div className="shrink-0 overflow-x-auto border-b" role="tablist" aria-label={label}>
      <div className={cn(PAGE_RAIL, PAGE_GUTTER, "flex items-center gap-6")}>
        {tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={active === tab.id}
            onClick={() => onSelect(tab.id)}
            className={cn(
              "relative shrink-0 py-3 text-body font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
              active === tab.id
                ? "text-foreground after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-foreground"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {tab.label}
          </button>
        ))}
      </div>
    </div>
  );
}

export function ActorChip({
  type,
  name,
  className,
}: {
  type: ActorType;
  name: string;
  className?: string;
}) {
  const Icon = type === "agent" ? Bot : type === "plugin" ? Puzzle : Cog;
  return (
    <span className={cn("inline-flex min-w-0 items-center gap-1.5", className)}>
      <Avatar size="sm" className="size-4">
        <AvatarFallback className="text-[9px]">
          {type === "member" ? (
            name.slice(0, 1).toUpperCase()
          ) : (
            <Icon className="size-2.5" aria-hidden="true" />
          )}
        </AvatarFallback>
      </Avatar>
      <span className="truncate">
        {name}
        {type === "agent" ? "（agent）" : ""}
      </span>
    </span>
  );
}

/** The small "#7" token used everywhere a revision is named. */
export function RevisionNumber({
  n,
  className,
}: {
  n: number;
  className?: string;
}) {
  return (
    <span className={cn("font-mono tabular-nums", className)}>#{n}</span>
  );
}
