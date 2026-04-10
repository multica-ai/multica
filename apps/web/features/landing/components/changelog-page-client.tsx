"use client";

import { LandingHeader } from "./landing-header";
import { LandingFooter } from "./landing-footer";
import { useLocale } from "../i18n";

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
  const { t } = useLocale();
  const categoryLabels = t.changelog.categories;

  return (
    <>
      <LandingHeader variant="light" />
      <main className="bg-white text-[#0a0d12]">
        <div className="mx-auto max-w-[720px] px-4 py-16 sm:px-6 sm:py-20 lg:py-24">
          <h1 className="font-[family-name:var(--font-serif)] text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem]">
            {t.changelog.title}
          </h1>
          <p className="mt-4 text-[15px] leading-7 text-[#0a0d12]/60 sm:text-[16px]">
            {t.changelog.subtitle}
          </p>

          <div className="mt-16 space-y-16">
            {t.changelog.entries.map((release) => {
              const hasCategorized =
                release.features || release.improvements || release.fixes;

              return (
                <div key={release.version} className="relative">
                  <div className="flex items-baseline gap-3">
                    <span className="text-[13px] font-semibold tabular-nums">
                      v{release.version}
                    </span>
                    <span className="text-[13px] text-[#0a0d12]/40">
                      {release.date}
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
                </div>
              );
            })}
          </div>
        </div>
      </main>
      <LandingFooter />
    </>
  );
}
