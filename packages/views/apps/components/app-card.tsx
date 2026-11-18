"use client";

import type { ReactNode } from "react";
import { ComposioToolkitLogo } from "../../common/composio-toolkit-logo";
import { AppLink } from "../../navigation";

/**
 * One app in the directory. The name is the card's link and covers the whole
 * card; `action` sits above it so its button keeps its own click.
 */
export function AppCard({
  slug,
  name,
  logo,
  href,
  subtitle,
  action,
  children,
}: {
  slug: string;
  name: string;
  logo?: string;
  href: string;
  subtitle?: ReactNode;
  action?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <li className="relative flex min-w-0 flex-col gap-2 rounded-lg border border-surface-border bg-surface p-3 transition-colors hover:bg-surface-hover">
      <div className="flex min-w-0 items-start gap-3">
        <ComposioToolkitLogo slug={slug} name={name} fallbackLogo={logo} />
        <div className="min-w-0 flex-1">
          <AppLink
            href={href}
            newTabTitle={name}
            className="block truncate text-body font-medium outline-none after:absolute after:inset-0 after:rounded-lg focus-visible:after:ring-2 focus-visible:after:ring-ring"
          >
            {name}
          </AppLink>
          {subtitle ? <p className="truncate text-caption text-muted-foreground">{subtitle}</p> : null}
        </div>
        {action ? <div className="relative z-10 shrink-0">{action}</div> : null}
      </div>
      {children ? <div className="min-w-0 space-y-1 text-caption text-muted-foreground">{children}</div> : null}
    </li>
  );
}
