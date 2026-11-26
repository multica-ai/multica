"use client";

import type { ReactNode } from "react";
import { ChevronRight } from "@multica/ui/icons";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { AppLink } from "../../navigation";

const CARD_CLASS =
  "group/app-card relative flex h-16 min-w-0 items-center gap-3 rounded-lg border border-surface-border bg-surface px-3";

/**
 * One app in the directory. Every card is the same height whatever it holds —
 * the name, at most one `meta` line, and one trailing slot — so the grid's rows
 * line up. The name is the card's link and covers the whole card; `action`
 * sits above it so its controls keep their own clicks. A card without an
 * action shows a chevron on hover instead.
 */
export function AppCard({
  slug,
  name,
  namePending = false,
  logo,
  href,
  meta,
  action,
}: {
  slug: string;
  name: string;
  /** The display name is still loading; `name` is only the slug, so it is read out but not shown. */
  namePending?: boolean;
  logo?: string;
  href: string;
  /** One line under the name; it must truncate itself. */
  meta?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <li className={`${CARD_CLASS} transition-colors hover:bg-surface-hover`}>
      <ComposioToolkitLogo slug={slug} name={name} fallbackLogo={logo} className="h-9 w-9 rounded-sm" />
      <div className="min-w-0 flex-1">
        <AppLink
          href={href}
          newTabTitle={name}
          className="block truncate text-body font-medium outline-none after:absolute after:inset-0 after:rounded-lg focus-visible:after:ring-2 focus-visible:after:ring-ring"
        >
          {namePending ? (
            <>
              <span className="sr-only">{name}</span>
              <Skeleton aria-hidden="true" className="my-[3px] h-3.5 w-24" />
            </>
          ) : (
            name
          )}
        </AppLink>
        {meta ? <p className="flex min-w-0 text-caption text-muted-foreground">{meta}</p> : null}
      </div>
      {action ? (
        <div className="relative z-10 flex shrink-0 items-center">{action}</div>
      ) : (
        <ChevronRight
          aria-hidden="true"
          className="size-4 shrink-0 text-faint-foreground opacity-0 transition-opacity group-focus-within/app-card:opacity-100 group-hover/app-card:opacity-100"
        />
      )}
    </li>
  );
}

/** A card-shaped placeholder, so the grid keeps its rows while the catalog loads. */
export function AppCardSkeleton() {
  return (
    <li aria-hidden="true" className={CARD_CLASS}>
      <Skeleton className="size-9 shrink-0 rounded-sm" />
      <div className="min-w-0 flex-1 space-y-2">
        <Skeleton className="h-3.5 w-2/5" />
        <Skeleton className="h-3 w-1/4" />
      </div>
    </li>
  );
}
