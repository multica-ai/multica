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

          {/* Right column — highlight grid */}
          <div className="flex-1">
            <div className="grid gap-px overflow-hidden rounded-2xl border border-[#0a0d12]/8 bg-[#0a0d12]/8 sm:grid-cols-2">
              {t.openSource.highlights.map((item) => (
                <div key={item.title} className="bg-white p-8 lg:p-10">
                  <h3 className="text-[17px] font-semibold leading-snug text-[#0a0d12] sm:text-[18px]">
                    {item.title}
                  </h3>
                  <p className="mt-3 text-[14px] leading-[1.7] text-[#0a0d12]/56 sm:text-[15px]">
                    {item.description}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
