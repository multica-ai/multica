"use client";

import Link from "next/link";
import { useAuthStore } from "@multica/core/auth";
import { useLocale } from "../i18n";
import { heroButtonClassName } from "./shared";

export function HowItWorksSection() {
  const { t } = useLocale();
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

        <div className="mt-20 grid gap-px overflow-hidden rounded-2xl border border-white/10 bg-white/10 sm:grid-cols-2 lg:grid-cols-4">
          {t.howItWorks.steps.map((step, i) => (
            <div
              key={i}
              className="flex flex-col bg-[#05070b] p-8 lg:p-10"
            >
              <span className="text-[13px] font-semibold tabular-nums text-white/28">
                {String(i + 1).padStart(2, "0")}
              </span>
              <h3 className="mt-4 text-[17px] font-semibold leading-snug text-white sm:text-[18px]">
                {step.title}
              </h3>
              <p className="mt-3 text-[14px] leading-[1.7] text-white/50 sm:text-[15px]">
                {step.description}
              </p>
            </div>
          ))}
        </div>

        <div className="mt-14 flex flex-wrap items-center gap-4">
          <Link href={user ? "/" : "/login"} className={heroButtonClassName("solid")}>
            {user ? t.header.dashboard : t.howItWorks.cta}
          </Link>
        </div>
      </div>
    </section>
  );
}
