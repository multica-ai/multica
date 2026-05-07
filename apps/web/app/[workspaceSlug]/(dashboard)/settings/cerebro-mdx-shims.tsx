// CEREBRO-PATCH(docs-panel-mdx-shims): MDX component shims for in-app docs panel.
//
// Upstream's `docs-panel.tsx` only ships a `Callout` shim. Cerebro's docs
// content (`apps/web/content/docs/index.mdx`) additionally uses editorial
// components (`NumberedCards`, `NumberedCard`, `NumberedSteps`, `Step`) that
// originated in `apps/docs/components/editorial.tsx`, which was deleted in
// upstream's bilingual-docs rewrite (commit 8c2e0841) without removing the
// in-app MDX usages. Without these shims the in-app Documentation tab throws
// "Expected component `NumberedCard` to be defined" at MDX render time.
//
// Lives outside docs-panel.tsx (rather than inline) so the upstream file
// stays a 1-line patch, keeping future upstream merges clean.

import Link from "next/link";
import type { ReactNode } from "react";

export function Callout({ children }: { children: ReactNode }) {
  return (
    <div className="my-4 rounded-md border border-l-4 border-l-primary bg-muted/40 px-4 py-3 text-sm">
      {children}
    </div>
  );
}

export function NumberedCards({ children }: { children: ReactNode }) {
  return <div className="not-prose my-6 grid grid-cols-1 gap-3 md:grid-cols-3">{children}</div>;
}

export function NumberedCard({
  number,
  title,
  href,
  tag,
  children,
}: {
  number?: string;
  title: string;
  href: string;
  tag?: string;
  children: ReactNode;
}) {
  return (
    <Link
      href={href}
      className="group flex flex-col gap-2 rounded-md border border-border bg-card p-4 no-underline transition-colors hover:bg-muted/50"
    >
      {number ? (
        <div className="font-mono text-[0.6875rem] uppercase tracking-wider text-muted-foreground">
          No. {number}
        </div>
      ) : null}
      <div className="text-[1rem] font-medium text-foreground group-hover:text-primary">{title}</div>
      <div className="text-sm text-muted-foreground">{children}</div>
      {tag ? (
        <div className="mt-1 font-mono text-[0.625rem] uppercase tracking-wider text-primary">{tag}</div>
      ) : null}
    </Link>
  );
}

export function NumberedSteps({ children }: { children: ReactNode }) {
  return <div className="not-prose my-6 divide-y divide-border border-y border-border">{children}</div>;
}

export function Step({ number, title, children }: { number: string; title: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[3rem_1fr] gap-4 py-4">
      <div className="text-2xl font-medium leading-none text-primary">{number}</div>
      <div>
        <div className="mb-1 text-base font-medium text-foreground">{title}</div>
        <div className="text-sm text-muted-foreground">{children}</div>
      </div>
    </div>
  );
}

export const cerebroMdxComponents = {
  Callout,
  NumberedCards,
  NumberedCard,
  NumberedSteps,
  Step,
};
