"use client";

import {
  type MouseEvent,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { cn } from "@multica/ui/lib/utils";
import { Container, Eyebrow } from "../site/primitives";
import { PageHero, SitePage } from "../site/site-page";
import { useLocale } from "../i18n";
import type { Locale } from "../i18n/types";

type ParsedDate = { year: number; month: number; day: number };

function parseDate(dateStr: string): ParsedDate {
  const parts = dateStr.split("-");
  return {
    year: Number(parts[0]),
    month: Number(parts[1]),
    day: Number(parts[2]),
  };
}

function utcDate(year: number, month: number, day: number) {
  const date = new Date(Date.UTC(year, month - 1, day));
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  ) {
    return null;
  }
  return date;
}

export function monthYearLabel(year: number, month: number, locale: Locale) {
  const date = utcDate(year, month, 1);
  if (!date) return "";
  return new Intl.DateTimeFormat(locale, {
    month: "long",
    timeZone: "UTC",
    year: "numeric",
  }).format(date);
}

export function fullDateLabel(dateStr: string, locale: Locale) {
  const { year, month, day } = parseDate(dateStr);
  const date = utcDate(year, month, day);
  if (!date) return dateStr;
  return new Intl.DateTimeFormat(locale, {
    day: "numeric",
    month: "long",
    timeZone: "UTC",
    year: "numeric",
  }).format(date);
}

type Release = {
  version: string;
  date: string;
  title: string;
  changes: string[];
  features?: string[];
  improvements?: string[];
  fixes?: string[];
};

type MonthGroup = {
  key: string;
  year: number;
  month: number;
  entries: Release[];
};

function groupByMonth(entries: readonly Release[]): MonthGroup[] {
  const groups: MonthGroup[] = [];
  for (const entry of entries) {
    const { year, month } = parseDate(entry.date);
    const key = `${year}-${month}`;
    const last = groups[groups.length - 1];
    if (last && last.key === key) {
      last.entries.push(entry);
    } else {
      groups.push({ key, year, month, entries: [entry] });
    }
  }
  return groups;
}

export function anchorId(version: string) {
  return `release-${version.replace(/\./g, "-")}`;
}

function ChangeList({ items }: { items: string[] }) {
  return (
    <ul className="mt-3 flex flex-col gap-2.5">
      {items.map((change) => (
        <li key={change} className="flex items-start gap-3 text-site-body text-site-ink-secondary">
          <span aria-hidden className="mt-[0.6em] size-1 shrink-0 rounded-full bg-site-line-strong" />
          {change}
        </li>
      ))}
    </ul>
  );
}

function ChangeGroup({ label, items }: { label: string; items?: string[] }) {
  if (!items || items.length === 0) return null;
  return (
    <div>
      <Eyebrow>{label}</Eyebrow>
      <ChangeList items={items} />
    </div>
  );
}

export function ChangelogPageClient() {
  const { t, locale } = useLocale();
  const categoryLabels = t.changelog.categories;
  const entries = t.changelog.entries;
  const groups = useMemo(() => groupByMonth(entries), [entries]);

  const [activeVersion, setActiveVersion] = useState<string>(
    entries[0]?.version ?? ""
  );
  const navLockRef = useRef<number | null>(null);

  useEffect(() => {
    if (entries.length === 0) return;
    const visible = new Set<string>();

    const observer = new IntersectionObserver(
      (observed) => {
        observed.forEach((e) => {
          const v = (e.target as HTMLElement).dataset.version;
          if (!v) return;
          if (e.isIntersecting) visible.add(v);
          else visible.delete(v);
        });
        // Ignore observer updates while we're programmatically scrolling
        // to a clicked target — otherwise the active indicator flickers
        // through each passing entry.
        if (navLockRef.current !== null) return;

        const firstVisible = entries.find((r) => visible.has(r.version));
        if (firstVisible) {
          setActiveVersion(firstVisible.version);
          return;
        }
        const scrollY = window.scrollY;
        let best = entries[0]?.version ?? "";
        for (const r of entries) {
          const el = document.getElementById(anchorId(r.version));
          if (!el) continue;
          if (el.getBoundingClientRect().top + scrollY <= scrollY + 160) {
            best = r.version;
          }
        }
        setActiveVersion(best);
      },
      { rootMargin: "-20% 0px -70% 0px", threshold: 0 }
    );

    entries.forEach((r) => {
      const el = document.getElementById(anchorId(r.version));
      if (el) observer.observe(el);
    });
    return () => observer.disconnect();
  }, [entries]);

  const jumpTo =
    (version: string) => (e: MouseEvent<HTMLAnchorElement>) => {
      const el = document.getElementById(anchorId(version));
      if (!el) return;
      e.preventDefault();
      el.scrollIntoView({ behavior: "smooth", block: "start" });
      window.history.replaceState(null, "", `#${anchorId(version)}`);
      setActiveVersion(version);
      if (navLockRef.current !== null) {
        window.clearTimeout(navLockRef.current);
      }
      navLockRef.current = window.setTimeout(() => {
        navLockRef.current = null;
      }, 800);
    };

  return (
    <SitePage>
      <PageHero title={t.changelog.title} lead={t.changelog.subtitle} />
      <Container className="pb-site-section lg:grid lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] lg:gap-16">
        <aside className="hidden lg:block">
          <nav aria-label={t.changelog.toc} className="sticky top-28 max-h-[calc(100vh-8rem)] overflow-y-auto pr-2 pb-8">
            <Eyebrow>{t.changelog.toc}</Eyebrow>
            <ol className="mt-5 flex flex-col gap-5 border-l border-site-line">
              {groups.map((group) => (
                <li key={group.key}>
                  <p className="pl-5 text-site-small text-site-muted">{monthYearLabel(group.year, group.month, locale)}</p>
                  <ol className="mt-1.5">
                    {group.entries.map((release) => {
                      const isActive = release.version === activeVersion;
                      return (
                        <li key={release.version}>
                          <a
                            href={`#${anchorId(release.version)}`}
                            onClick={jumpTo(release.version)}
                            aria-current={isActive ? "true" : undefined}
                            className={cn(
                              "relative -ml-px flex items-baseline gap-3 border-l py-1 pl-5 text-site-small tabular-nums transition-colors",
                              isActive
                                ? "border-site-ink text-site-ink"
                                : "border-transparent text-site-muted hover:text-site-ink",
                            )}
                          >
                            <span className="w-5 text-right">{parseDate(release.date).day}</span>
                            <span>v{release.version}</span>
                          </a>
                        </li>
                      );
                    })}
                  </ol>
                </li>
              ))}
            </ol>
          </nav>
        </aside>

        <div className="min-w-0 border-t border-site-line">
          {entries.map((release) => {
            const hasCategorized = release.features || release.improvements || release.fixes;
            return (
              <section
                key={release.version}
                id={anchorId(release.version)}
                data-version={release.version}
                className="scroll-mt-28 border-b border-site-line py-site-block"
              >
                <p className="flex items-baseline gap-3 text-site-small tabular-nums">
                  <span className="text-site-ink">v{release.version}</span>
                  <span className="text-site-muted">{fullDateLabel(release.date, locale)}</span>
                </p>
                <h2 className="mt-3 max-w-[40rem] font-site-display text-site-title text-site-ink text-balance">
                  {release.title}
                </h2>
                {hasCategorized ? (
                  <div className="mt-6 flex max-w-[40rem] flex-col gap-6">
                    <ChangeGroup label={categoryLabels.features} items={release.features} />
                    <ChangeGroup label={categoryLabels.improvements} items={release.improvements} />
                    <ChangeGroup label={categoryLabels.fixes} items={release.fixes} />
                  </div>
                ) : (
                  <div className="mt-6 max-w-[40rem]">
                    <ChangeList items={release.changes} />
                  </div>
                )}
              </section>
            );
          })}
        </div>
      </Container>
    </SitePage>
  );
}
