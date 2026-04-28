"use client";

import {
  type MouseEvent,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { LandingHeader } from "./landing-header";
import { LandingFooter } from "./landing-footer";
import { useLocale } from "../i18n";
import type { Locale } from "../i18n/types";

const MONTHS_EN = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];

type ParsedDate = { year: number; month: number; day: number };

function parseDate(dateStr: string): ParsedDate {
  const parts = dateStr.split("-");
  return {
    year: Number(parts[0]),
    month: Number(parts[1]),
    day: Number(parts[2]),
  };
}

function monthYearLabel(year: number, month: number, locale: Locale) {
  if (!year || !month) return "";
  if (locale === "zh-TW") return `${year}\u5e74${month}\u6708`;
  return `${MONTHS_EN[month - 1]} ${year}`;
}

function fullDateLabel(dateStr: string, locale: Locale) {
  const { year, month, day } = parseDate(dateStr);
  if (!year || !month || !day) return dateStr;
  if (locale === "zh-TW") return `${year}\u5e74${month}\u6708${day}\u65e5`;
  return `${MONTHS_EN[month - 1]} ${day}, ${year}`;
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

function anchorId(version: string) {
  return `release-${version.replace(/\./g, "-")}`;
}

function ChangeList({ items }: { items: string[] }) {
  return (
    <ul className="mt-2 space-y-2">
      {items.map((change) => (
        <li
          key={change}
          className="flex items-start gap-2.5 text-[14px] leading-[1.7] text-[#0a0d12]/60 sm:text-[15px]"
        >
          <span className="mt-2.5 h-1 w-1 shrink-0 rounded-full bg-[#0a0d12]/30" />
          {change}
        </li>
      ))}
    </ul>
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
    <>
      <LandingHeader variant="light" />
      <main className="bg-white text-[#0a0d12]">
        <div className="mx-auto max-w-[1080px] px-4 py-16 sm:px-6 sm:py-20 lg:py-24">
          <div className="lg:grid lg:grid-cols-[200px_minmax(0,1fr)] lg:gap-16">
            <aside className="hidden lg:block">
              <nav
                aria-label={t.changelog.toc}
                className="sticky top-28 max-h-[calc(100vh-8rem)] overflow-y-auto pb-8 pr-2"
              >
                <h3 className="text-[11px] font-semibold uppercase tracking-[0.14em] text-[#0a0d12]/50">
                  {t.changelog.toc}
                </h3>

                <div className="relative mt-5">
                  <span
                    aria-hidden="true"
                    className="pointer-events-none absolute left-[4px] top-7 bottom-2 w-px bg-[#0a0d12]/10"
                  />

                  <ol className="space-y-5">
                    {groups.map((group) => (
                      <li key={group.key}>
                        <p className="ml-6 text-[11px] font-semibold uppercase tracking-[0.12em] text-[#0a0d12]/45">
                          {monthYearLabel(group.year, group.month, locale)}
                        </p>

                        <ol className="mt-1.5">
                          {group.entries.map((release) => {
                            const isActive =
                              release.version === activeVersion;
                            const { day } = parseDate(release.date);
                            return (
                              <li key={release.version}>
                                <a
                                  href={`#${anchorId(release.version)}`}
                                  onClick={jumpTo(release.version)}
                                  aria-current={isActive ? "true" : undefined}
                                  className={[
                                    "group relative flex items-center gap-3 rounded-md py-1 pr-2 text-[13px] transition-colors",
                                    isActive
                                      ? "text-[#0a0d12]"
                                      : "text-[#0a0d12]/55 hover:text-[#0a0d12]/80",
                                  ].join(" ")}
                                >
                                  <span
                                    aria-hidden="true"
                                    className={[
                                      "relative z-10 block size-[9px] shrink-0 rounded-full border transition-all duration-200",
                                      isActive
                                        ? "border-[#0a0d12] bg-[#0a0d12] ring-4 ring-[#0a0d12]/8"
                                        : "border-[#0a0d12]/25 bg-white group-hover:border-[#0a0d12]/60",
                                    ].join(" ")}
                                  />
                                  <span
                                    className={[
                                      "w-[1.25rem] shrink-0 text-right tabular-nums",
                                      isActive
                                        ? "font-semibold"
                                        : "font-medium",
                                    ].join(" ")}
                                  >
                                    {day}
                                  </span>
                                  <span className="tabular-nums text-[11px] text-[#0a0d12]/35">
                                    v{release.version}
                                  </span>
                                </a>
                              </li>
                            );
                          })}
                        </ol>
                      </li>
                    ))}
                  </ol>
                </div>
              </nav>
            </aside>

            <div className="mx-auto min-w-0 max-w-[720px] lg:mx-0">
              <h1 className="font-[family-name:var(--font-serif)] text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem]">
                {t.changelog.title}
              </h1>
              <p className="mt-4 text-[15px] leading-7 text-[#0a0d12]/60 sm:text-[16px]">
                {t.changelog.subtitle}
              </p>

              <div className="mt-16 space-y-16">
                {entries.map((release) => {
                  const hasCategorized =
                    release.features || release.improvements || release.fixes;
                  return (
                    <section
                      key={release.version}
                      id={anchorId(release.version)}
                      data-version={release.version}
                      className="relative scroll-mt-28"
                    >
                      <div className="flex items-baseline gap-3">
                        <span className="text-[13px] font-semibold tabular-nums">
                          v{release.version}
                        </span>
                        <span className="text-[13px] text-[#0a0d12]/40">
                          {fullDateLabel(release.date, locale)}
                        </span>
                      </div>
                      <h2 className="mt-2 text-[20px] font-semibold leading-snug sm:text-[22px]">
                        {release.title}
                      </h2>

                      {hasCategorized ? (
                        <div className="mt-4 space-y-5">
                          {release.features && release.features.length > 0 && (
                            <div>
                              <h3 className="text-[13px] font-semibold uppercase tracking-wide text-[#0a0d12]/50">
                                {categoryLabels.features}
                              </h3>
                              <ChangeList items={release.features} />
                            </div>
                          )}
                          {release.improvements &&
                            release.improvements.length > 0 && (
                              <div>
                                <h3 className="text-[13px] font-semibold uppercase tracking-wide text-[#0a0d12]/50">
                                  {categoryLabels.improvements}
                                </h3>
                                <ChangeList items={release.improvements} />
                              </div>
                            )}
                          {release.fixes && release.fixes.length > 0 && (
                            <div>
                              <h3 className="text-[13px] font-semibold uppercase tracking-wide text-[#0a0d12]/50">
                                {categoryLabels.fixes}
                              </h3>
                              <ChangeList items={release.fixes} />
                            </div>
                          )}
                        </div>
                      ) : (
                        <ChangeList items={release.changes} />
                      )}
                    </section>
                  );
                })}
              </div>
            </div>
          </div>
        </div>
      </main>
      <LandingFooter />
    </>
  );
}
