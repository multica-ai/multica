"use client";

import Image from "next/image";
import Link from "next/link";
import { ArrowRight, Download } from "lucide-react";
import { useAuthStore } from "@multica/core/auth";
import { docsHrefForLocale, useLocale } from "../i18n";
import { useDashboardCtaHref } from "../utils/use-dashboard-cta";
import { HERO_PROVIDERS } from "./provider-marks";
import { heroButtonClassName } from "./shared";

export function LandingHero() {
  const { t, locale } = useLocale();
  const user = useAuthStore((s) => s.user);
  const ctaHref = useDashboardCtaHref();

  return (
    <div className="relative min-h-full overflow-hidden bg-[#05070b] text-white">
      <LandingBackdrop />

      <main className="relative z-10">
        <section
          id="product"
          className="mx-auto max-w-[1320px] px-4 pb-16 pt-28 sm:px-6 sm:pt-32 lg:px-8 lg:pb-24 lg:pt-36"
        >
          <div className="mx-auto max-w-[1120px] text-center">
            <h1 className="landing-serif text-[3.65rem] leading-[0.93] tracking-[-0.038em] text-white drop-shadow-[0_10px_34px_rgba(0,0,0,0.32)] sm:text-[4.85rem] lg:text-[6.4rem]">
              {t.hero.headlineLine1}
              <br />
              {t.hero.headlineLine2}
            </h1>

            <p className="mx-auto mt-7 max-w-[820px] text-body-lg leading-7 text-white/84 sm:text-title">
              {t.hero.subheading}
            </p>

            <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
              <Link href={ctaHref} className={heroButtonClassName("solid")}>
                {user ? t.header.dashboard : t.hero.cta}
              </Link>
              <Link
                href="/download"
                className={heroButtonClassName("ghost")}
              >
                <Download className="size-4" aria-hidden />
                {t.hero.downloadDesktop}
              </Link>
              <Link
                href="/contact-sales"
                className="group inline-flex items-center justify-center gap-1.5 rounded-(--landing-radius-action) px-3 py-3 text-body font-semibold text-white/80 transition-colors hover:text-white"
              >
                {t.hero.talkToSales}
                <ArrowRight
                  className="size-4 transition-transform group-hover:translate-x-0.5"
                  aria-hidden
                />
              </Link>
            </div>
          </div>

          <WorksWithRow
            label={t.hero.worksWith}
            href={`${docsHrefForLocale(locale)}/providers`}
          />

          <div id="preview" className="mt-10 sm:mt-12">
            <ProductImage alt={t.hero.imageAlt} />
          </div>
        </section>
      </main>
    </div>
  );
}

/**
 * The runtime catalog is far longer than this row (see `/docs/providers`), so
 * the marks are a sample and the label carries the full claim — that split is
 * what keeps the row honest without growing it every time a runtime lands.
 */
function WorksWithRow({ label, href }: { label: string; href: string }) {
  return (
    <div className="mt-12 flex flex-col items-center gap-6">
      <Link
        href={href}
        className="group inline-flex items-center gap-1.5 text-body text-white/60 transition-colors hover:text-white"
      >
        {label}
        <ArrowRight
          className="size-3.5 transition-transform group-hover:translate-x-0.5"
          aria-hidden
        />
      </Link>

      {/*
        Explicit column counts, not free wrapping: with `flex-wrap` the 14 marks
        broke 13 + 1 around 834px and 12 + 2 around 768px, and a single orphan
        on the second row reads as a bug rather than a layout. Fourteen columns
        once there is room for one row, seven — an exact 7 x 2 — below that. The
        columns carry the spacing, so the marks stay evenly pitched at every
        width and the grid still shrinks below its max width on a narrow phone.
      */}
      <ul className="grid w-full max-w-[392px] grid-cols-7 items-center justify-items-center gap-y-6 sm:max-w-[532px] lg:max-w-[896px] lg:grid-cols-14">
        {HERO_PROVIDERS.map(({ name, Mark, size }) => (
          <li
            key={name}
            title={name}
            className="flex items-center text-white opacity-70 drop-shadow-[0_1px_6px_rgba(0,0,0,0.28)] transition-opacity duration-200 hover:opacity-100"
          >
            <Mark className={size ?? "size-6"} />
            <span className="sr-only">{name}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function LandingBackdrop() {
  return (
    <div className="pointer-events-none absolute inset-0">
      {/* This artwork is above the fold, so preload it alongside the product preview. */}
      <Image
        src="/images/landing-bg.webp"
        alt=""
        fill
        preload
        className="object-cover object-center"
        sizes="100vw"
      />
    </div>
  );
}

function ProductImage({ alt }: { alt: string }) {
  return (
    <div>
      <div className="relative overflow-hidden border border-white/14">
        <Image
          src="/images/landing-hero.webp"
          alt={alt}
          width={2640}
          height={1781}
          preload
          className="block h-auto w-full"
          sizes="(max-width: 1320px) 100vw, 1320px"
          quality={85}
        />
      </div>
    </div>
  );
}
