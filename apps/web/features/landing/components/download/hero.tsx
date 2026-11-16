"use client";

import { useLocale } from "../../i18n";
import { Container, Eyebrow, SiteTextLink, actionClassName } from "../../site/primitives";
import { alternateKey, formatSize, osOf, resolvePrimary } from "../../utils/download-target";
import type { LatestRelease } from "../../utils/github-release";
import type { DetectResult } from "../../utils/os-detect";
import type { DownloadKey } from "../../utils/parse-release-assets";
import { ALL_RELEASES_URL, AllPlatforms } from "./all-platforms";
import { DownloadLink } from "./download-link";
import { OSIcon } from "./os-icons";

/**
 * The download page's first screen: the visitor's installer on the left,
 * every other build beside it. The platform comes from the server, so the
 * first paint already offers the right file.
 */
export function DownloadHero({
  detected,
  release,
  onStart,
}: {
  detected: DetectResult;
  release: LatestRelease;
  onStart: (key: DownloadKey) => void;
}) {
  const { t, locale } = useLocale();
  const d = t.download.hero;
  const primary = resolvePrimary(detected, release.assets);
  const os = primary ? osOf(primary.key) : null;
  // Only a guessed architecture needs the other one offered beside it.
  const alternate = primary && !detected.archConfident ? alternateKey(primary.key) : null;
  const alternateHref = alternate ? release.assets[alternate] : undefined;
  const size = primary ? release.sizes[primary.key] : undefined;
  const lead = detected.os === "mobile" ? d.mobileNote : primary ? d.lead : d.leadOther;

  return (
    <Container className="grid gap-10 pt-site-hero pb-site-section lg:grid-cols-2 lg:items-center lg:gap-16">
      <div>
        <Eyebrow>{d.eyebrow}</Eyebrow>
        <h1 className="mt-3 font-site-display text-site-heading text-site-ink text-balance">
          {os ? d.title[os] : d.title.other}
        </h1>
        <p className="mt-4 max-w-[34rem] text-site-body text-site-muted text-pretty">{lead}</p>
        {primary && os ? (
          <div className="mt-7 flex flex-wrap gap-2.5">
            <DownloadLink
              downloadKey={primary.key}
              href={primary.href}
              onStart={onStart}
              className={actionClassName("primary", "lg")}
            >
              <OSIcon os={os} size={16} aria-hidden />
              {d.primary[primary.key]}
            </DownloadLink>
            {alternate && alternateHref ? (
              <DownloadLink
                downloadKey={alternate}
                href={alternateHref}
                onStart={onStart}
                className={actionClassName("secondary", "lg")}
              >
                {d.alternate[alternate]}
              </DownloadLink>
            ) : null}
          </div>
        ) : null}
        <p className="mt-4 flex flex-wrap items-center gap-x-3 gap-y-1 text-site-small text-site-muted">
          {release.version ? (
            <>
              <span>{release.version}</span>
              {size ? <Dot>{formatSize(size)}</Dot> : null}
              {release.publishedAt ? <Dot>{d.released.replace("{date}", formatDate(release.publishedAt, locale))}</Dot> : null}
              <Dot>
                <SiteTextLink
                  href={release.htmlUrl ?? ALL_RELEASES_URL}
                  external
                  className="text-site-ink underline decoration-site-line-strong underline-offset-4"
                >
                  {d.whatsNew}
                </SiteTextLink>
              </Dot>
            </>
          ) : (
            <span>{d.versionUnavailable}</span>
          )}
        </p>
      </div>
      <AllPlatforms assets={release.assets} current={primary?.key} onStart={onStart} />
    </Container>
  );
}

function Dot({ children }: { children: React.ReactNode }) {
  return (
    <>
      <span aria-hidden className="text-site-faint">
        ·
      </span>
      <span>{children}</span>
    </>
  );
}

/** In UTC, so the server's render and the browser's agree. */
function formatDate(iso: string, locale: string) {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeZone: "UTC" }).format(new Date(iso));
}
