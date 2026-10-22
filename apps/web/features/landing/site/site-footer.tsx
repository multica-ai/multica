"use client";

import { useMemo } from "react";
import Link from "next/link";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
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
import { Container, SiteTextLink, focusRing } from "./primitives";
import { buildFooterGroups } from "./sitemap";

const SOCIAL_LINKS = [
  { label: "GitHub", href: githubUrl, Mark: GitHubMark },
  { label: "Discord", href: discordUrl, Mark: DiscordMark },
  { label: "X", href: twitterUrl, Mark: XMark },
];

export function SiteFooter() {
  const { locale, setLocale } = useLocale();
  const groups = useMemo(() => buildFooterGroups(docsHrefForLocale(locale)), [locale]);

  return (
    <footer className="border-t border-site-line bg-site-canvas text-site-ink">
      <Container className="grid gap-12 py-16 lg:grid-cols-[minmax(0,1fr)_minmax(0,3fr)] lg:gap-16 lg:py-20">
        <div>
          <Link href="/" aria-label="Multica home" className={cn("inline-flex items-center gap-2.5 rounded-sm", focusRing)}>
            <MulticaIcon className="size-[18px]" noSpin />
            <span className="text-site-logo font-medium lowercase">multica</span>
          </Link>
          <p className="mt-4 max-w-[18rem] text-site-body text-site-muted">
            The workspace for human and agent teams.
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
                    "inline-flex size-9 items-center justify-center rounded-lg text-site-muted transition-colors hover:bg-site-hover hover:text-site-ink",
                    focusRing,
                  )}
                >
                  <Mark className="size-4" />
                </Link>
              </li>
            ))}
          </ul>
        </div>

        <nav aria-label="Footer" className="grid grid-cols-2 gap-x-6 gap-y-10 sm:grid-cols-3 lg:grid-cols-5">
          {groups.map((group) => (
            <div key={group.title}>
              <p className="text-site-eyebrow text-site-muted">{group.title}</p>
              <ul className="mt-3 flex flex-col gap-1">
                {group.links.map((link) => (
                  <li key={link.label}>
                    <SiteTextLink
                      href={link.href}
                      external={link.external}
                      className="py-1 text-site-body text-site-ink-secondary hover:text-site-ink"
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
        <div className="flex flex-col gap-4 border-t border-site-line py-6 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-site-eyebrow text-site-muted">
            © {new Date().getFullYear()} Multica. All rights reserved.
          </p>
          <div role="group" aria-label="Language" className="flex items-center gap-1">
            {locales.map((l) => (
              <button
                type="button"
                key={l}
                aria-pressed={l === locale}
                onClick={() => setLocale(l)}
                className={cn(
                  "rounded-md px-2 py-1 text-site-eyebrow transition-colors",
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
        </div>
      </Container>
    </footer>
  );
}
