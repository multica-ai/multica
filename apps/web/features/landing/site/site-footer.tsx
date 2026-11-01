"use client";

import { useMemo } from "react";
import Link from "next/link";
import { cn } from "@multica/ui/lib/utils";
import { docsHrefForLocale, localeLabels, locales, useLocale } from "../i18n";
import {
  DiscordMark,
  GitHubMark,
  XMark,
  discordUrl,
  githubUrl,
  twitterUrl,
} from "../components/shared";
import { BrandWordmark } from "./brand-logo";
import { FooterWordmark } from "./footer-wordmark";
import { Container, SiteTextLink, focusRing } from "./primitives";
import { buildFooterGroups } from "./sitemap";

const SOCIAL_LINKS = [
  { label: "GitHub", href: githubUrl, Mark: GitHubMark },
  { label: "Discord", href: discordUrl, Mark: DiscordMark },
  { label: "X", href: twitterUrl, Mark: XMark },
];

/** One column per group on wide screens, so a short footer doesn't leave empty columns. */
const WIDE_COLUMNS: Record<number, string> = { 3: "lg:grid-cols-3", 4: "lg:grid-cols-4" };

export function SiteFooter() {
  const { locale, setLocale, t } = useLocale();
  const labels = t.site.footer;
  const groups = useMemo(() => buildFooterGroups(docsHrefForLocale(locale), t.site.nav), [locale, t]);

  return (
    <footer className="border-t border-site-rule bg-site-canvas text-site-ink">
      <Container className="grid gap-site-block py-site-section lg:grid-cols-[minmax(0,1fr)_minmax(0,3fr)] lg:gap-16">
        <div>
          <Link href="/" aria-label={t.site.header.home} className={cn("group inline-flex items-center rounded-sm", focusRing)}>
              <BrandWordmark />
          </Link>
          <p className="mt-4 max-w-[18rem] text-site-small text-site-muted">
            {labels.tagline}
          </p>
          <ul className="mt-6 flex items-center gap-1">
            {SOCIAL_LINKS.map(({ label, href, Mark }) => (
              <li key={label}>
                <Link
                  href={href}
                  target="_blank"
                  rel="noreferrer"
                  aria-label={label}
                  className={cn(
                    "inline-flex size-8 items-center justify-center rounded-site-control text-site-muted transition-colors hover:bg-site-hover hover:text-site-ink",
                    focusRing,
                  )}
                >
                  <Mark className="size-4" />
                </Link>
              </li>
            ))}
          </ul>
          <div role="group" aria-label={labels.language} className="mt-6 flex flex-wrap items-center gap-1">
            {locales.map((l) => (
              <button
                type="button"
                key={l}
                aria-pressed={l === locale}
                onClick={() => setLocale(l)}
                className={cn(
                  "rounded-site-control px-2.5 py-1 text-site-small transition-colors",
                  focusRing,
                  l === locale
                    ? "bg-site-hover text-site-ink"
                    : "text-site-muted hover:text-site-ink",
                )}
              >
                {localeLabels[l]}
              </button>
            ))}
          </div>
          <p className="mt-4 text-site-small text-site-muted">
            {labels.copyright.replace("{year}", String(new Date().getFullYear()))}
          </p>
        </div>

        <nav
          aria-label={labels.navigation}
          className={cn("grid grid-cols-2 gap-x-4 gap-y-site-block sm:grid-cols-3", WIDE_COLUMNS[groups.length] ?? "lg:grid-cols-5")}
        >
          {groups.map((group) => (
            <div key={group.title}>
              <p className="text-site-small text-site-ink">{group.title}</p>
              <ul className="mt-2 flex flex-col gap-1">
                {group.links.map((link) => (
                  <li key={link.label}>
                    <SiteTextLink
                      href={link.href}
                      external={link.external}
                      className="py-0.5 text-site-small text-site-muted hover:text-site-ink"
                    >
                      {link.label}
                    </SiteTextLink>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </nav>
      </Container>

      <Container>
        <FooterWordmark className="pb-10" />
      </Container>
    </footer>
  );
}
