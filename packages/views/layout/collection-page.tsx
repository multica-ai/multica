"use client";

import type { ComponentProps, ReactNode } from "react";
import {
  ChevronDown,
  Filter,
  Plus,
  SlidersHorizontal,
  type IconComponent,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { PageHeader } from "./page-header";

interface CollectionPageHeaderProps {
  icon: IconComponent;
  /** The scope being looked at, e.g. "All agents" — not the bare noun. */
  title: ReactNode;
  /**
   * Items of the title's menu, usually `CollectionScopeItems`. Without one the
   * title is plain text.
   */
  menu?: ReactNode;
  /** Filter and search, right after the title. */
  tools?: ReactNode;
  /** Icon controls on the right: display settings, then create. */
  actions?: ReactNode;
  className?: string;
}

/**
 * The one header row of a list page, laid out like the issues header: the
 * title names the scope and opens the scope menu, filter and search follow
 * it, and the right side holds icon controls whose names live in tooltips.
 * Counts belong to the sidebar rows, so the row carries none.
 */
export function CollectionPageHeader({
  icon: Icon,
  title,
  menu,
  tools,
  actions,
  className,
}: CollectionPageHeaderProps) {
  const label = (
    <>
      <Icon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate">{title}</span>
    </>
  );
  return (
    <PageHeader className={className}>
      <div className="flex min-w-0 flex-1 items-center gap-1">
        {menu ? (
          <DropdownMenu>
            <h1 className="flex min-w-20">
              <DropdownMenuTrigger
                render={
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 min-w-0 max-w-64 shrink gap-2 px-2 text-body font-medium text-foreground"
                  />
                }
              >
                {label}
                <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
              </DropdownMenuTrigger>
            </h1>
            <DropdownMenuContent align="start" className="w-auto min-w-52">
              {menu}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <h1 className="flex min-w-0 items-center gap-2 px-2 text-body font-medium">
            {label}
          </h1>
        )}
        {tools}
      </div>
      {actions ? (
        <div className="flex shrink-0 items-center gap-1">{actions}</div>
      ) : null}
    </PageHeader>
  );
}

export interface CollectionScopeOption<T extends string> {
  value: T;
  label: ReactNode;
  /** Shown in the menu only; the header never carries a count. */
  count?: number;
}

export function CollectionScopeItems<T extends string>({
  value,
  options,
  onChange,
}: {
  value: T;
  options: CollectionScopeOption<T>[];
  onChange: (value: T) => void;
}) {
  return (
    <DropdownMenuRadioGroup value={value} onValueChange={(next) => onChange(next as T)}>
      {options.map((option) => (
        <DropdownMenuRadioItem key={option.value} value={option.value}>
          <span className="flex-1">{option.label}</span>
          {option.count !== undefined ? (
            <span className="ml-3 text-caption tabular-nums text-muted-foreground">
              {option.count}
            </span>
          ) : null}
        </DropdownMenuRadioItem>
      ))}
    </DropdownMenuRadioGroup>
  );
}

type HeaderIconButtonProps = Omit<ComponentProps<typeof IconButton>, "label" | "children"> & {
  /** Overrides the default tooltip and accessible name. */
  label?: string;
};

/**
 * The filter control: an icon that turns into a count once filters are on.
 * Pass it as the `render` of the filter menu's trigger.
 */
export function CollectionFilterButton({
  count = 0,
  label,
  className,
  ...props
}: HeaderIconButtonProps & { count?: number }) {
  const { t } = useT("layout");
  return (
    <IconButton
      {...props}
      size="sm"
      tooltipSide="bottom"
      label={
        label ??
        (count > 0
          ? t(($) => $.collection.filter_count, { count })
          : t(($) => $.collection.filter))
      }
      className={cn(
        "h-7 min-w-7 shrink-0 gap-1.5 px-1.5 text-muted-foreground",
        count > 0 && "text-foreground",
        className,
      )}
    >
      <Filter className="size-4" />
      {count > 0 ? (
        <span className="rounded-sm bg-brand/10 px-1 text-caption font-medium tabular-nums text-brand">
          {count}
        </span>
      ) : null}
    </IconButton>
  );
}

/** Clears the filters and search; render it only while one is on. */
export function CollectionResetButton({ onClick }: { onClick: () => void }) {
  const { t } = useT("layout");
  return (
    <Button
      variant="ghost"
      size="sm"
      className="h-7 shrink-0 px-2 text-muted-foreground"
      onClick={onClick}
    >
      {t(($) => $.collection.reset)}
    </Button>
  );
}

/**
 * Display settings (sort, columns, layout). Pass it as the `render` of the
 * popover that holds them; `icon` shows the current layout when there is one.
 */
export function CollectionDisplayButton({
  icon: Icon = SlidersHorizontal,
  label,
  className,
  ...props
}: HeaderIconButtonProps & { icon?: IconComponent }) {
  const { t } = useT("layout");
  return (
    <IconButton
      {...props}
      tooltipSide="bottom"
      label={label ?? t(($) => $.collection.display)}
      className={cn("shrink-0 text-muted-foreground", className)}
    >
      <Icon />
    </IconButton>
  );
}

/** Create is always an icon; the page's one solid button is never "New". */
export function CollectionCreateButton({
  label,
  className,
  ...props
}: Omit<ComponentProps<typeof IconButton>, "children">) {
  return (
    <IconButton
      {...props}
      tooltipSide="bottom"
      label={label}
      className={cn("shrink-0 text-muted-foreground", className)}
    >
      <Plus />
    </IconButton>
  );
}

type PageStateTone = "muted" | "destructive" | "warning";

interface CollectionPageStateProps {
  icon: IconComponent;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  tone?: PageStateTone;
  role?: "alert" | "status";
  className?: string;
}

const stateToneClass: Record<PageStateTone, string> = {
  muted: "text-muted-foreground",
  destructive: "text-destructive",
  warning: "text-warning",
};

/** Shared centered state for collection empty, error and not-found views. */
export function CollectionPageState({
  icon: Icon,
  title,
  description,
  actions,
  tone = "muted",
  role,
  className,
}: CollectionPageStateProps) {
  return (
    <Empty
      role={role}
      className={cn("rounded-none border-0 px-6 py-16", className)}
    >
      <EmptyHeader>
        <EmptyMedia
          variant="icon"
          className={cn(
            "size-12 rounded-full [&_svg]:size-6",
            stateToneClass[tone],
          )}
        >
          <Icon aria-hidden="true" />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        {description ? (
          <EmptyDescription className="max-w-md">{description}</EmptyDescription>
        ) : null}
      </EmptyHeader>
      {actions ? (
        <EmptyContent className="mt-1 flex-row justify-center">
          {actions}
        </EmptyContent>
      ) : null}
    </Empty>
  );
}
