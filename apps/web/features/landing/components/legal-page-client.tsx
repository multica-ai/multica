"use client";

import Link from "next/link";
import { LandingHeader } from "./landing-header";
import { LandingFooter } from "./landing-footer";
import { useLocale } from "../i18n";

type LegalPageProps = {
  title: string;
  paragraphs: string[];
};

export function LegalPageClient({ title, paragraphs }: LegalPageProps) {
  const { locale } = useLocale();

  return (
    <>
      <LandingHeader variant="light" />
      <main className="bg-white text-[#0a0d12]">
        <div className="mx-auto max-w-[720px] px-4 py-16 sm:px-6 sm:py-20 lg:py-24">
          <h1 className="font-[family-name:var(--font-serif)] text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem]">
            {title}
          </h1>
          <div className="mt-8 space-y-6 text-[15px] leading-[1.8] text-[#0a0d12]/70 sm:text-[16px]">
            {paragraphs.map((p, i) => (
              <p key={i}>{p}</p>
            ))}
          </div>

          <div className="mt-12">
            <Link
              href={`/${locale}`}
              className="inline-flex items-center gap-2.5 rounded-[12px] bg-[#0a0d12] px-5 py-3 text-[14px] font-semibold text-white transition-colors hover:bg-[#0a0d12]/88"
            >
              Back to home
            </Link>
          </div>
        </div>
      </main>
      <LandingFooter />
    </>
  );
}
