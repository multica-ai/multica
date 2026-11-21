"use client";

import { useEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import {
  ChevronDown,
  Filter,
  Plus,
  Search,
  SlidersHorizontal,
  X,
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
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@multica/ui/components/ui/input-group";
import { cn } from "@multica/ui/lib/utils";
import { useModalStore } from "@multica/core/modals";
import {
  createShortcutChord,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
} from "@multica/core/shortcuts";
import { isImeComposing } from "@multica/core/utils";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
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
 * Below this width of the header's left group, an open search cannot share
 * the row with the title: title, filter, field and Reset only fit together
 * from here up. Narrower, as on a phone, the title steps aside while the
 * search is open so every control keeps its own box (MUL-8490). The scope
 * stays one tap away in the nav.
 */
const TITLE_YIELDS_TO_SEARCH =
  "@max-[22rem]/collection-tools:group-has-[[data-collection-search=open]]/collection-tools:hidden";

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
      <div className="group/collection-tools @container/collection-tools flex min-w-0 flex-1 items-center gap-1">
        {menu ? (
          <DropdownMenu>
            <h1 className={cn("flex min-w-20", TITLE_YIELDS_TO_SEARCH)}>
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
          <h1
            className={cn(
              "flex min-w-0 items-center gap-2 px-2 text-body font-medium",
              TITLE_YIELDS_TO_SEARCH,
            )}
          >
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

const SEARCH_SHORTCUT = createShortcutChord("/");

interface CollectionSearchProps {
  value: string;
  onChange: (value: string) => void;
  /**
   * Names the search, e.g. "Search agents": the icon's tooltip, the field's
   * accessible name and its placeholder.
   */
  label: string;
}

/**
 * List search for the header's tools: an icon until it is used, then a field.
 * "/" opens it from anywhere on the page. Esc or the clear button empties and
 * closes it; while it holds a query it stays open, so a query kept in the view
 * store is still visible after coming back to the list. In a narrow
 * `CollectionPageHeader` the title steps aside while it is open.
 */
export function CollectionSearch({
  value,
  onChange,
  label,
}: CollectionSearchProps) {
  const { t } = useT("layout");
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const focusTriggerOnCloseRef = useRef(false);
  const expanded = open || value !== "";

  // Only opening focuses the field: a query restored on return must not take
  // focus from the list, whose j/k keys need it on the page.
  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  // Clearing with the button removes it; hand focus to the icon that replaces
  // the field instead of dropping it on the body.
  useEffect(() => {
    if (expanded || !focusTriggerOnCloseRef.current) return;
    focusTriggerOnCloseRef.current = false;
    rootRef.current?.querySelector("button")?.focus();
  }, [expanded]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "/" || event.defaultPrevented || event.repeat) return;
      if (event.metaKey || event.ctrlKey || event.altKey || isImeComposing(event)) return;
      if (isEditableShortcutTarget(event.target)) return;
      if (isPortalLayerShortcutTarget(event.target)) return;
      if (useModalStore.getState().modal) return;
      // A page kept mounted in a hidden tab must not take the key.
      const root = rootRef.current;
      if (!root || root.getClientRects().length === 0) return;
      event.preventDefault();
      setOpen(true);
      inputRef.current?.focus();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  const close = () => {
    onChange("");
    setOpen(false);
  };

  return (
    <div
      ref={rootRef}
      data-collection-search={expanded ? "open" : "closed"}
      className={cn(
        "flex items-center",
        // The open field takes the room left after the title, up to 240px.
        // Its size limits live on this flex item, never on a child: a child
        // wider than its slot spills over Reset and the actions instead of
        // making the title shrink (MUL-8490).
        expanded
          ? "min-w-0 max-w-60 flex-1 @min-[22rem]/collection-tools:min-w-32"
          : "shrink-0",
      )}
    >
      {expanded ? (
        <InputGroup
          className="h-7 w-full"
          onFocus={() => setOpen(true)}
          onBlur={(event) => {
            if (event.currentTarget.contains(event.relatedTarget as Node | null)) return;
            // Switching windows blurs too but keeps the element active; the
            // field should still be open on return.
            if (document.activeElement === event.target) return;
            setOpen(false);
          }}
        >
          <InputGroupAddon>
            <Search aria-hidden="true" className="size-3.5" />
          </InputGroupAddon>
          <InputGroupInput
            ref={inputRef}
            value={value}
            onChange={(event) => onChange(event.target.value)}
            onKeyDown={(event) => {
              if (event.key !== "Escape" || isImeComposing(event)) return;
              event.preventDefault();
              close();
              event.currentTarget.blur();
            }}
            aria-label={label}
            aria-keyshortcuts="/"
            placeholder={label}
            autoComplete="off"
            spellCheck={false}
            className="h-full"
          />
          {value ? (
            <InputGroupAddon align="inline-end">
              <IconButton
                size="icon-xs"
                tooltipSide="bottom"
                label={t(($) => $.collection.clear_search)}
                className="text-muted-foreground"
                onClick={() => {
                  focusTriggerOnCloseRef.current = true;
                  close();
                }}
              >
                <X />
              </IconButton>
            </InputGroupAddon>
          ) : null}
        </InputGroup>
      ) : (
        <IconButton
          label={label}
          shortcut={<ShortcutKeycaps shortcut={SEARCH_SHORTCUT} decorative />}
          tooltipSide="bottom"
          aria-keyshortcuts="/"
          className="shrink-0 text-muted-foreground"
          onClick={() => setOpen(true)}
        >
          <Search />
        </IconButton>
      )}
    </div>
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
  /** One solid button at most: on an empty page it is the page's one action. */
  actions?: ReactNode;
  /** Below the actions: a quiet link, or content such as starter templates. */
  children?: ReactNode;
  tone?: PageStateTone;
  /**
   * `compact` is for rows hidden by filters or search inside a list that still
   * shows its header; `default` is for a page with nothing to show.
   */
  size?: "default" | "compact";
  role?: "alert" | "status";
  className?: string;
}

const stateToneClass: Record<PageStateTone, string> = {
  muted: "text-muted-foreground",
  destructive: "bg-destructive/10 text-destructive",
  warning: "bg-warning/15 text-warning",
};

/**
 * The centered state of a page or list with nothing to show: a first-run
 * empty collection, rows hidden by filters, a missing object or a load error.
 * A first run says what the collection is for and offers to create one; the
 * others always offer a way out (clear, go back, retry).
 */
export function CollectionPageState({
  icon: Icon,
  title,
  description,
  actions,
  children,
  tone = "muted",
  size = "default",
  role,
  className,
}: CollectionPageStateProps) {
  const compact = size === "compact";
  return (
    <Empty
      role={role}
      className={cn(
        "rounded-none border-0 px-6",
        compact ? "gap-3.5 py-12" : "py-16",
        className,
      )}
    >
      <EmptyHeader className={compact ? "gap-0.5" : "gap-1"}>
        <EmptyMedia
          variant="icon"
          className={cn(
            "rounded-full",
            compact ? "mb-2.5 size-10 [&_svg]:size-5" : "mb-3 size-12 [&_svg]:size-6",
            stateToneClass[tone],
          )}
        >
          <Icon aria-hidden="true" />
        </EmptyMedia>
        <EmptyTitle className={compact ? undefined : "text-title-sm font-semibold"}>
          {title}
        </EmptyTitle>
        {description ? (
          <EmptyDescription className={cn("max-w-md", compact && "text-label")}>
            {description}
          </EmptyDescription>
        ) : null}
      </EmptyHeader>
      {actions ? (
        <EmptyContent className={cn("flex-row flex-wrap justify-center gap-2", !compact && "mt-1")}>
          {actions}
        </EmptyContent>
      ) : null}
      {children}
    </Empty>
  );
}

/**
 * A list whose filters or search hide every row. It never offers to create:
 * the rows exist, they are only hidden, so its one action clears what hides
 * them.
 */
export function CollectionNoMatches({
  title,
  description,
  onReset,
  hasFilters = false,
  hasSearch = false,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  /** Clears the filters and search; omit when neither is on. */
  onReset?: () => void;
  hasFilters?: boolean;
  hasSearch?: boolean;
  children?: ReactNode;
}) {
  const { t } = useT("layout");
  const resetLabel =
    hasFilters && hasSearch
      ? t(($) => $.collection.clear_filters_and_search)
      : hasSearch
        ? t(($) => $.collection.clear_search)
        : t(($) => $.collection.clear_filters);
  return (
    <CollectionPageState
      size="compact"
      icon={Search}
      title={title}
      description={description}
      actions={
        onReset && (hasFilters || hasSearch) ? (
          <Button type="button" variant="outline" size="sm" onClick={onReset}>
            {resetLabel}
          </Button>
        ) : null
      }
    >
      {children}
    </CollectionPageState>
  );
}
