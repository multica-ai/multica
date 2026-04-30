"use client";

import Link from "next/link";
import { useLocale } from "../i18n";
import { GitHubMark, githubUrl } from "./shared";

export function OpenSourceSection() {
  const { t } = useLocale();

  return (
    <section id="open-source" className="bg-white text-[#0a0d12]">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <div className="flex flex-col gap-16 lg:flex-row lg:items-start lg:gap-24">
          {/* Left column — heading + CTA */}
          <div className="lg:w-[480px] lg:shrink-0">
            <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#0a0d12]/40">
              {t.openSource.label}
            </p>
            <h2 className="mt-4 font-[family-name:var(--font-serif)] text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem] lg:text-[4.2rem]">
              {t.openSource.headlineLine1}
              <br />
              {t.openSource.headlineLine2}
            </h2>
            <p className="mt-6 max-w-[420px] text-[15px] leading-7 text-[#0a0d12]/60 sm:text-[16px]">
              {t.openSource.description}
            </p>
            <div className="mt-8 flex flex-wrap items-center gap-3">
              <Link
                href={githubUrl}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center justify-center gap-2.5 rounded-[12px] bg-[#0a0d12] px-5 py-3 text-[14px] font-semibold text-white transition-colors hover:bg-[#0a0d12]/88"
              >
                <GitHubMark className="size-4" />
                {t.openSource.cta}
              </Link>
            </div>
          </div>

          {/* Right column — narrative highlight list */}
          <ul className="flex-1 divide-y divide-[#0a0d12]/8">
            {t.openSource.highlights.map((item, i) => (
              <li
                key={item.title}
                className={
                  i === 0
                    ? "pb-8 sm:pb-10"
                    : i === t.openSource.highlights.length - 1
                      ? "pt-8 sm:pt-10"
                      : "py-8 sm:py-10"
                }
              >
                <div className="flex flex-col gap-3 sm:flex-row sm:items-baseline sm:gap-10">
                  <h3
                    className={
                      i === 0
                        ? "text-[22px] font-semibold leading-tight tracking-[-0.01em] text-[#0a0d12] sm:text-[24px] sm:w-[260px] sm:shrink-0"
                        : "text-[17px] font-semibold leading-snug text-[#0a0d12] sm:text-[18px] sm:w-[260px] sm:shrink-0"
                    }
                  >
                    {item.title}
                  </h3>
                  <p className="text-[14px] leading-[1.7] text-[#0a0d12]/60 sm:text-[15px]">
                    {item.description}
                  </p>
                </div>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  );
}
