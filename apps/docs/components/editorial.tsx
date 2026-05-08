"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { useDocsLocale } from "@/components/locale-link";
import { prefixLocale } from "@/lib/locale-link";

/**
 * Byline — editorial metadata strip with ruled top + bottom borders.
 *
 * Sits below DocsHero on showpiece pages (welcome). Carries the small
 * uppercase metadata: section · updated · read time. Mirrors the v2
 * editorial pattern of a "by-line" between title and body, separating
 * the heading hero from the article proper.
 */
export function Byline({ items }: { items: string[] }) {
  return (
    <div className="not-prose mb-9 flex items-center gap-3.5 border-y border-[var(--docs-rule)] py-3.5 text-xs uppercase tracking-[0.08em] text-muted-foreground">
      {items.map((item, i) => (
        <span key={i} className="flex items-center gap-3.5">
          {i > 0 ? (
            <span className="size-[3px] rounded-full bg-[var(--docs-faint)]" />
          ) : null}
          <span>{item}</span>
        </span>
      ))}
    </div>
  );
}

/**
 * NumberedCards — three-column ruled-divider grid with No.01/02/03 serif
 * numbers. Showpiece component; replaces fumadocs's <Cards> on the welcome
 * page. Top + bottom heavy rules frame the row.
 */
export function NumberedCards({ children }: { children: ReactNode }) {
  return (
    <div className="not-prose my-9 grid grid-cols-1 border-y border-[var(--docs-rule)] md:grid-cols-3">
      {children}
    </div>
  );
}

/**
 * NumberedCard — child of NumberedCards. Internally numbered by CSS counter,
 * but we also accept an explicit `number` prop in case the consumer wants
 * to override (e.g. start at "03").
 */
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
  const lang = useDocsLocale();
  return (
    <Link
      href={prefixLocale(href, lang)}
      className="group flex flex-col gap-2.5 border-r border-border px-0 py-5 pr-4 no-underline last:border-r-0 md:px-4 md:first:pl-0 md:last:pr-0"
    >
      <div className="font-mono text-[0.6875rem] uppercase tracking-[0.08em] text-muted-foreground">
        {number ? `No. ${number}` : null}
      </div>
      <div className="font-[family-name:var(--font-serif)] text-[1.375rem] leading-[1.25] tracking-[-0.015em] text-foreground transition-colors group-hover:text-[var(--primary)]">
        {title}
      </div>
      <div className="text-[0.84375rem] leading-[1.55] text-muted-foreground">
        {children}
      </div>
      {tag ? (
        <div className="mt-1 font-mono text-[0.625rem] uppercase tracking-[0.06em] text-[var(--primary)]">
          {tag}
        </div>
      ) : null}
    </Link>
  );
}

/**
 * NumberedSteps — large serif step numbers, ruled-row separators.
 * Use for sequential walkthroughs (install → login → start → assign).
 */
export function NumberedSteps({ children }: { children: ReactNode }) {
  return <div className="not-prose my-7 border-t border-border">{children}</div>;
}

export function Step({
  number,
  title,
  children,
}: {
  number: string;
  title: string;
  children: ReactNode;
}) {
  return (
    <div className="grid grid-cols-[3.5rem_1fr] gap-5 border-b border-border py-5">
      <div className="font-[family-name:var(--font-serif)] text-[2rem] font-normal leading-none tracking-[-0.02em] text-[var(--primary)]">
        {number}
      </div>
      <div>
        <div className="mb-1 font-[family-name:var(--font-serif)] text-[1.25rem] leading-[1.3] tracking-[-0.01em] text-foreground">
          {title}
        </div>
        <div className="text-[0.9375rem] leading-[1.6] text-muted-foreground">
          {children}
        </div>
      </div>
    </div>
  );
}
