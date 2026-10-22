"use client";

import Link from "next/link";
import { LandingHeader } from "./landing-header";
import { LandingFooter } from "./landing-footer";
import { GitHubMark, githubUrl } from "./shared";
import { InlineLinks } from "./inline-links";
import { useLocale } from "../i18n";

export function AboutPageClient() {
  const { t } = useLocale();
  const n = t.about.nameLine;
  const team = t.about.team;

  return (
    <>
      <LandingHeader variant="light" />
      <main className="bg-white text-[#0a0d12]">
        <div className="mx-auto max-w-[720px] px-4 py-16 sm:px-6 sm:py-20 lg:py-24">
          <h1 className="landing-serif text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem]">
            {t.about.title}
          </h1>
          <div className="mt-8 space-y-6 text-body-lg leading-[1.8] text-[#0a0d12]/70 sm:text-title-sm">
            <p>
              {n.prefix}
              <strong className="font-semibold text-[#0a0d12]">
                {n.mult}
              </strong>
              {n.iplexed}
              <strong className="font-semibold text-[#0a0d12]">
                {n.i}
              </strong>
              {n.nformationAnd}
              <strong className="font-semibold text-[#0a0d12]">
                {n.c}
              </strong>
              {n.omputing}
              <strong className="font-semibold text-[#0a0d12]">
                {n.a}
              </strong>
              {n.gent}
            </p>
            {t.about.paragraphs.map((p, i) => (
              <p key={i}>{p}</p>
            ))}
          </div>

          <div className="mt-12">
            <Link
              href={githubUrl}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-2.5 rounded-(--landing-radius-action) bg-[#0a0d12] px-5 py-3 text-body font-semibold text-white transition-colors hover:bg-[#0a0d12]/88"
            >
              <GitHubMark className="size-4" />
              {t.about.cta}
            </Link>
          </div>

          <section className="mt-16 border-t border-[#0a0d12]/10 pt-12 sm:mt-20 sm:pt-16">
            <h2 className="landing-serif text-[2rem] leading-[1.1] tracking-[-0.02em] sm:text-[2.4rem]">
              {team.title}
            </h2>
            <div className="mt-6 space-y-6 text-body-lg leading-[1.8] text-[#0a0d12]/70 sm:text-title-sm">
              {team.paragraphs.map((p, i) => (
                <p key={i}>
                  <InlineLinks text={p} />
                </p>
              ))}
            </div>
            <dl className="mt-10 grid gap-px overflow-hidden rounded-2xl border border-[#0a0d12]/8 bg-[#0a0d12]/8 sm:grid-cols-2">
              {team.contacts.map((contact) => {
                const external = contact.href.startsWith("http");
                return (
                  <div key={contact.label} className="bg-white p-6">
                    <dt className="text-caption font-semibold uppercase tracking-[0.1em] text-[#0a0d12]/40">
                      {contact.label}
                    </dt>
                    <dd className="mt-2">
                      <Link
                        href={contact.href}
                        {...(external
                          ? { target: "_blank", rel: "noreferrer" }
                          : {})}
                        className="text-body-lg font-semibold text-[#0a0d12] underline decoration-[#0a0d12]/20 underline-offset-4 transition-colors hover:decoration-[#0a0d12]"
                      >
                        {contact.linkLabel}
                      </Link>
                    </dd>
                  </div>
                );
              })}
            </dl>
          </section>
        </div>
      </main>
      <LandingFooter />
    </>
  );
}
