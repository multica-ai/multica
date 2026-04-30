"use client";

import Link from "next/link";
import { useAuthStore } from "@multica/core/auth";
import { useLocale } from "../i18n";
import { GitHubMark, githubUrl, heroButtonClassName } from "./shared";

export function HowItWorksSection() {
  const { t, locale } = useLocale();
  const user = useAuthStore((s) => s.user);

  return (
    <section id="how-it-works" className="bg-[#05070b] text-white">
      <div className="mx-auto max-w-[1320px] px-4 py-24 sm:px-6 sm:py-32 lg:px-8 lg:py-40">
        <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-white/40">
          {t.howItWorks.label}
        </p>
        <h2 className="mt-4 font-[family-name:var(--font-serif)] text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem] lg:text-[4.2rem]">
          {t.howItWorks.headlineMain}
          <br />
          <span className="text-white/40">{t.howItWorks.headlineFaded}</span>
        </h2>

        {/* Desktop: horizontal step flow with large serif numerals + connecting rule */}
        <ol className="mt-20 hidden lg:grid lg:grid-cols-4 lg:gap-10">
          {t.howItWorks.steps.map((step, i) => (
            <li key={i} className="relative flex flex-col">
              <span className="font-[family-name:var(--font-serif)] text-[5rem] leading-none tracking-[-0.04em] text-white/72">
                {i + 1}
              </span>
              <div className="mt-6 flex items-center gap-3">
                <span className="h-px flex-1 bg-white/14" />
                {i < t.howItWorks.steps.length - 1 ? (
                  <svg
                    aria-hidden="true"
                    width="14"
                    height="10"
                    viewBox="0 0 14 10"
                    fill="none"
                    className="text-white/30"
                  >
                    <path
                      d="M9 1l4 4-4 4M0 5h13"
                      stroke="currentColor"
                      strokeWidth="1"
                      strokeLinecap="square"
                    />
                  </svg>
                ) : (
                  <span className="size-1.5 shrink-0 rounded-full bg-white/40" />
                )}
              </div>
              <h3 className="mt-6 text-[18px] font-semibold leading-snug text-white">
                {step.title}
              </h3>
              <p className="mt-3 text-[15px] leading-[1.7] text-white/56">
                {step.description}
              </p>
            </li>
          ))}
        </ol>

        {/* Mobile / tablet: vertical timeline with continuous left rail */}
        <ol className="mt-16 lg:hidden">
          {t.howItWorks.steps.map((step, i) => {
            const isLast = i === t.howItWorks.steps.length - 1;
            return (
              <li key={i} className="relative grid grid-cols-[3.25rem_1fr] gap-x-5">
                {/* Left rail + numeral */}
                <div className="relative flex flex-col items-start">
                  <span className="font-[family-name:var(--font-serif)] text-[3rem] leading-none tracking-[-0.03em] text-white/80">
                    {i + 1}
                  </span>
                  {!isLast && (
                    <span
                      aria-hidden="true"
                      className="absolute left-[1.05rem] top-[3.5rem] bottom-[-2.5rem] w-px bg-white/14"
                    />
                  )}
                </div>
                {/* Right content */}
                <div className={isLast ? "pb-0" : "pb-12"}>
                  <h3 className="text-[17px] font-semibold leading-snug text-white sm:text-[18px]">
                    {step.title}
                  </h3>
                  <p className="mt-2.5 text-[14px] leading-[1.7] text-white/56 sm:text-[15px]">
                    {step.description}
                  </p>
                </div>
              </li>
            );
          })}
        </ol>

        <div className="mt-14 flex flex-wrap items-center gap-4 lg:mt-20">
          <Link href={user ? "/" : "/login"} className={heroButtonClassName("solid")}>
            {user ? t.header.dashboard : t.howItWorks.cta}
          </Link>
          <Link
            href={locale === "zh" ? "/docs/zh" : "/docs"}
            className={heroButtonClassName("ghost")}
          >
            {t.howItWorks.ctaDocs}
          </Link>
          <Link
            href={githubUrl}
            target="_blank"
            rel="noreferrer"
            className={heroButtonClassName("ghost")}
          >
            <GitHubMark className="size-4" />
            {t.howItWorks.ctaGithub}
          </Link>
        </div>
      </div>
    </section>
  );
}
