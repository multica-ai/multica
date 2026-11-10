"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useLocale } from "@/features/landing/i18n";
import { SitePage } from "@/features/landing/site/site-page";
import { localizeHref } from "@/lib/landing-locale-path";
import { DownloadHero } from "@/features/landing/components/download/hero";
import { CliSection } from "@/features/landing/components/download/cli-section";
import { DownloadStarted } from "@/features/landing/components/download/started";
import { useDetectedPlatform } from "@/features/landing/components/download/use-detected-platform";
import { takePendingDownload } from "@/features/landing/utils/download-target";
import type { LatestRelease } from "@/features/landing/utils/github-release";
import type { DetectResult } from "@/features/landing/utils/os-detect";
import type { DownloadKey } from "@/features/landing/utils/parse-release-assets";

export function DownloadClient({
  release,
  platform,
  started: startedFromUrl,
}: {
  release: LatestRelease;
  platform: DetectResult;
  /** Set when a homepage download button sent the visitor here. */
  started: DownloadKey | null;
}) {
  const router = useRouter();
  const { locale } = useLocale();
  const detected = useDetectedPlatform(platform);
  const [started, setStarted] = useState(startedFromUrl);
  const startedHref = started ? release.assets[started] : undefined;

  // The homepage click left a pending flag; start that file once. A reload
  // or a shared link shows the same page without downloading again.
  useEffect(() => {
    const href = startedFromUrl ? release.assets[startedFromUrl] : undefined;
    if (startedFromUrl && href && takePendingDownload(startedFromUrl)) window.location.assign(href);
  }, [startedFromUrl, release.assets]);

  return (
    <SitePage>
      {started && startedHref ? (
        <DownloadStarted
          downloadKey={started}
          href={startedHref}
          size={release.sizes[started]}
          onBack={() => {
            setStarted(null);
            if (startedFromUrl) router.replace(localizeHref(locale, "/download"), { scroll: false });
          }}
        />
      ) : (
        <>
          <DownloadHero detected={detected} release={release} onStart={setStarted} />
          <CliSection />
        </>
      )}
    </SitePage>
  );
}
