"use client";

import type { ReactNode } from "react";
import { LandingHeader } from "./landing-header";
import { LandingFooter } from "./landing-footer";
import { InlineLinks } from "./inline-links";
import type { DocumentSection } from "../i18n";

// Shared shell for long-form pages (licensing, privacy): same reading column
// and type scale as the About page.
export function DocumentPage({
  title,
  meta,
  intro,
  children,
}: {
  title: string;
  meta?: string;
  intro: string[];
  children: ReactNode;
}) {
  return (
    <>
      <LandingHeader variant="light" />
      <main className="bg-white text-[#0a0d12]">
        <article className="mx-auto max-w-[720px] px-4 py-16 sm:px-6 sm:py-20 lg:py-24">
          <h1 className="landing-serif text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem]">
            {title}
          </h1>
          {meta && (
            <p className="mt-4 text-label text-[#0a0d12]/40">{meta}</p>
          )}
          <div className="mt-8 space-y-5 text-body-lg leading-[1.8] text-[#0a0d12]/70">
            {intro.map((p, i) => (
              <p key={i}>
                <InlineLinks text={p} />
              </p>
            ))}
          </div>
          {children}
        </article>
      </main>
      <LandingFooter />
    </>
  );
}

export function DocumentSections({ sections }: { sections: DocumentSection[] }) {
  return sections.map((section) => (
    <section key={section.heading} className="mt-12">
      <h2 className="text-title font-semibold leading-snug text-[#0a0d12]">
        {section.heading}
      </h2>
      {section.paragraphs?.map((p, i) => (
        <p
          key={i}
          className="mt-4 text-body-lg leading-[1.8] text-[#0a0d12]/70"
        >
          <InlineLinks text={p} />
        </p>
      ))}
      {section.bullets && (
        <ul className="mt-4 list-disc space-y-2 pl-5 text-body-lg leading-[1.8] text-[#0a0d12]/70 marker:text-[#0a0d12]/30">
          {section.bullets.map((b, i) => (
            <li key={i}>
              <InlineLinks text={b} />
            </li>
          ))}
        </ul>
      )}
    </section>
  ));
}
