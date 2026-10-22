"use client";

// Temporary: hero layout options for review, switched with `?hero=a|b|c|d|e|f`.
// Remove once a layout is chosen.

import { useEffect, useState } from "react";
import Link from "next/link";
import { ArrowRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { ActionLink, Container, ProductFrame, focusRing } from "./primitives";
import { PLACEHOLDER_PAGES } from "./sitemap";

type HeroVariant = "a" | "b" | "c" | "d" | "e" | "f";
const VARIANTS: HeroVariant[] = ["a", "b", "c", "d", "e", "f"];

export function useHeroVariant(): HeroVariant | null {
  const [variant, setVariant] = useState<HeroVariant | null>(null);
  useEffect(() => {
    const value = new URLSearchParams(window.location.search).get("hero");
    const match = VARIANTS.find((v) => v === value);
    if (match) setVariant(match);
  }, []);
  return variant;
}

function Announcement({ className }: { className?: string }) {
  return (
    <Link
      href={`/${PLACEHOLDER_PAGES.blog.slug}`}
      className={cn(
        "inline-flex items-center gap-2 rounded-site-control border border-site-line bg-site-surface py-1 pr-3 pl-1 text-site-small text-site-ink-secondary transition-colors hover:border-site-line-strong",
        focusRing,
        className,
      )}
    >
      <span className="rounded-site-control bg-site-sunken px-2 py-0.5 text-site-ink">New</span>
      Announcement placeholder
      <ArrowRight className="size-3.5 text-site-muted" aria-hidden />
    </Link>
  );
}

function Ctas({ href, label, className }: { href: string; label: string; className?: string }) {
  return (
    <div className={cn("flex flex-wrap gap-2.5", className)}>
      <ActionLink href={href} size="lg">
        {label}
      </ActionLink>
      <ActionLink href="/contact-sales" size="lg" tone="secondary">
        Book a demo
      </ActionLink>
    </div>
  );
}

export function HeroVariants({ variant, ctaHref, ctaLabel }: { variant: HeroVariant; ctaHref: string; ctaLabel: string }) {
  if (variant === "a") {
    // Same stack, tightened: one-line headline, two-line lead, half the gaps.
    return (
      <section className="pt-12 pb-site-section lg:pt-16">
        <Container>
          <Announcement />
          <h1 className="mt-5 font-site-display text-site-heading text-site-ink">Your next ten hires won’t be human</h1>
          <p className="mt-3 max-w-[38rem] text-site-body text-site-muted text-pretty">
            Placeholder tagline. Assign issues to coding agents, review their work, and scale the team — on
            your machines or in the cloud.
          </p>
          <Ctas href={ctaHref} label={ctaLabel} className="mt-6" />
        </Container>
        <Container className="mt-10 lg:mt-12">
          <ProductFrame label="Hero product visual" aspectRatio="16 / 9" />
        </Container>
      </section>
    );
  }

  if (variant === "b") {
    // Split row: headline left, lead and actions right on a shared baseline.
    return (
      <section className="pt-12 pb-site-section lg:pt-16">
        <Container>
          <Announcement />
          <div className="mt-5 grid items-end gap-6 lg:grid-cols-[1.25fr_1fr] lg:gap-16">
            <h1 className="font-site-display text-site-display text-site-ink text-balance">
              Your next ten hires won’t be human
            </h1>
            <div>
              <p className="max-w-[30rem] text-site-body text-site-muted text-pretty">
                Placeholder tagline. Assign issues to coding agents, review their work, and scale the team — on
                your machines or in the cloud.
              </p>
              <Ctas href={ctaHref} label={ctaLabel} className="mt-5" />
            </div>
          </div>
        </Container>
        <Container className="mt-10 lg:mt-12">
          <ProductFrame label="Hero product visual" aspectRatio="16 / 9" />
        </Container>
      </section>
    );
  }

  if (variant === "e" || variant === "f") {
    // Headline and actions left, description right: its last line level with
    // the headline's last line (e) or with the bottom of the actions (f).
    return (
      <section className="pt-12 pb-site-section lg:pt-16">
        <Container>
          <Announcement />
          <div className="mt-5 grid gap-x-16 gap-y-6 lg:grid-cols-[1.25fr_1fr]">
            <h1 className="font-site-display text-site-display text-site-ink text-balance lg:col-start-1 lg:row-start-1">
              Your next ten hires won’t be human
            </h1>
            <p
              className={cn(
                "max-w-[30rem] text-site-body text-site-muted text-pretty lg:col-start-2 lg:row-start-1 lg:self-end lg:justify-self-end",
                variant === "f" && "lg:row-span-2",
              )}
            >
              Placeholder tagline. Assign issues to coding agents, review their work, and scale the team — on
              your machines or in the cloud.
            </p>
            <Ctas href={ctaHref} label={ctaLabel} className="lg:col-start-1 lg:row-start-2" />
          </div>
        </Container>
        <Container className="mt-10 lg:mt-12">
          <ProductFrame label="Hero product visual" aspectRatio="16 / 9" />
        </Container>
      </section>
    );
  }

  if (variant === "c") {
    // Centred, one line each: short headline, one-line lead, product right under.
    return (
      <section className="pt-12 pb-site-section lg:pt-14">
        <Container className="flex flex-col items-center text-center">
          <Announcement />
          <h1 className="mt-5 font-site-display text-site-display text-site-ink text-balance">
            Your next hires won’t be human
          </h1>
          <p className="mt-4 max-w-[42rem] text-site-body text-site-muted text-pretty">
            Placeholder tagline. Coding agents that take issues, report progress, and ship PRs.
          </p>
          <Ctas href={ctaHref} label={ctaLabel} className="mt-6 justify-center" />
        </Container>
        <Container className="mt-10">
          <ProductFrame label="Hero product visual" aspectRatio="16 / 9" />
        </Container>
      </section>
    );
  }

  // d: text and product side by side; the product bleeds off the right edge.
  return (
    <section className="overflow-hidden pt-10 pb-site-section lg:pt-12">
      <Container className="grid items-center gap-10 lg:grid-cols-[5fr_7fr] lg:gap-12">
        <div>
          <Announcement />
          <h1 className="mt-5 font-site-display text-site-display text-site-ink text-balance">
            Your next ten hires won’t be human
          </h1>
          <p className="mt-4 max-w-[28rem] text-site-body text-site-muted text-pretty">
            Placeholder tagline. Assign issues to coding agents, review their work, and scale the team.
          </p>
          <Ctas href={ctaHref} label={ctaLabel} className="mt-6" />
        </div>
        <ProductFrame
          label="Hero product visual"
          aspectRatio="4 / 3"
          className="lg:mr-[calc((var(--site-gutter)_+_max(0px,(100vw_-_var(--site-container))_/_2))_*_-1)] lg:rounded-r-none"
        />
      </Container>
    </section>
  );
}
