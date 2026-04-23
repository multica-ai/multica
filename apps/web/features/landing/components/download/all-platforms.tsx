import Link from "next/link";
import {
  captureDownloadInitiated,
  type DownloadInitiatedPayload,
} from "@multica/core/analytics";
import { useLocale } from "../../i18n";
import type { DetectResult } from "../../utils/os-detect";
import type { DownloadAssets } from "../../utils/parse-release-assets";
import { AppleIcon, LinuxIcon, WindowsIcon } from "./os-icons";

type Platform = DownloadInitiatedPayload["platform"];
type Arch = DownloadInitiatedPayload["arch"];
type Format = DownloadInitiatedPayload["format"];

interface Props {
  assets: DownloadAssets;
  /** Link to GitHub releases page, used when individual asset URLs
   *  couldn't be resolved (API down / parse failure). */
  fallbackHref: string;
  /** Release tag (e.g. "v0.2.13"); null on fetch failure. */
  version: string | null;
  /** Current OS/arch guess. Used only to compute `matched_detect` on
   *  the download_initiated event — the row UI itself is static. */
  detected: DetectResult | null;
}

/**
 * Full matrix of platform + arch + format links. Always visible
 * regardless of which platform the Hero resolved to — lets power
 * users grab any build directly.
 */
export function AllPlatforms({
  assets,
  fallbackHref,
  version,
  detected,
}: Props) {
  const { t } = useLocale();
  const d = t.download.allPlatforms;

  const trackClick = (platform: Platform, arch: Arch, format: Format) => {
    if (!version) return;
    captureDownloadInitiated({
      platform,
      arch,
      format,
      version,
      // Manual pick from the matrix — Hero is the primary CTA.
      primary_cta: false,
      // True only when the row matches what we guessed client-side.
      // Lets us measure detect accuracy from the miss rate on this
      // event alone (no need to cross-join to download_page_viewed).
      matched_detect:
        !!detected &&
        detected.os === platform &&
        detected.arch === arch,
    });
  };

  return (
    <section
      id="all-platforms"
      className="bg-white py-20 text-[#0a0d12] sm:py-24"
    >
      <div className="mx-auto max-w-[920px] px-4 sm:px-6 lg:px-8">
        <h2 className="font-[family-name:var(--font-serif)] text-[2.2rem] leading-[1.1] tracking-[-0.03em] sm:text-[2.6rem]">
          {d.title}
        </h2>

        <div className="mt-10 overflow-hidden rounded-2xl border border-[#0a0d12]/10">
          <Row
            icon={<AppleIcon className="text-[#0a0d12]" />}
            label={d.macLabel}
            formats={[
              {
                label: d.formatDmg,
                href: assets.macArm64Dmg,
                onClick: () => trackClick("mac", "arm64", "dmg"),
              },
              {
                label: d.formatZip,
                href: assets.macArm64Zip,
                onClick: () => trackClick("mac", "arm64", "zip"),
              },
            ]}
            unavailable={d.unavailable}
          />
          <Row
            icon={<WindowsIcon className="text-[#0a0d12]" />}
            label={d.winX64Label}
            formats={[
              {
                label: d.formatExe,
                href: assets.winX64Exe,
                onClick: () => trackClick("windows", "x64", "exe"),
              },
            ]}
            unavailable={d.unavailable}
          />
          <Row
            icon={<WindowsIcon className="text-[#0a0d12]" />}
            label={d.winArm64Label}
            formats={[
              {
                label: d.formatExe,
                href: assets.winArm64Exe,
                onClick: () => trackClick("windows", "arm64", "exe"),
              },
            ]}
            unavailable={d.unavailable}
          />
          <Row
            icon={<LinuxIcon className="text-[#0a0d12]" />}
            label={d.linuxX64Label}
            formats={[
              {
                label: d.formatAppImage,
                href: assets.linuxAmd64AppImage,
                onClick: () => trackClick("linux", "x64", "appimage"),
              },
              {
                label: d.formatDeb,
                href: assets.linuxAmd64Deb,
                onClick: () => trackClick("linux", "x64", "deb"),
              },
              {
                label: d.formatRpm,
                href: assets.linuxAmd64Rpm,
                onClick: () => trackClick("linux", "x64", "rpm"),
              },
            ]}
            unavailable={d.unavailable}
          />
          <Row
            icon={<LinuxIcon className="text-[#0a0d12]" />}
            label={d.linuxArm64Label}
            formats={[
              {
                label: d.formatAppImage,
                href: assets.linuxArm64AppImage,
                onClick: () => trackClick("linux", "arm64", "appimage"),
              },
              {
                label: d.formatDeb,
                href: assets.linuxArm64Deb,
                onClick: () => trackClick("linux", "arm64", "deb"),
              },
              {
                label: d.formatRpm,
                href: assets.linuxArm64Rpm,
                onClick: () => trackClick("linux", "arm64", "rpm"),
              },
            ]}
            unavailable={d.unavailable}
            isLast
          />
        </div>

        <p className="mt-6 text-[13px] text-[#0a0d12]/60">{d.intelNote}</p>

        {isFallbackNeeded(assets) ? (
          <p className="mt-2 text-[13px] text-[#0a0d12]/60">
            <Link
              href={fallbackHref}
              className="underline decoration-[#0a0d12]/30 underline-offset-4 hover:text-[#0a0d12] hover:decoration-[#0a0d12]/70"
              target="_blank"
              rel="noreferrer"
            >
              {t.download.footer.allReleases}
            </Link>
          </p>
        ) : null}
      </div>
    </section>
  );
}

// ------------------------------------------------------------
// Row
// ------------------------------------------------------------

interface RowProps {
  icon: React.ReactNode;
  label: string;
  formats: {
    label: string;
    href: string | undefined;
    onClick: () => void;
  }[];
  unavailable: string;
  isLast?: boolean;
}

function Row({ icon, label, formats, unavailable, isLast }: RowProps) {
  return (
    <div
      className={`flex flex-wrap items-center gap-x-6 gap-y-3 px-6 py-5 ${isLast ? "" : "border-b border-[#0a0d12]/8"}`}
    >
      <div className="flex min-w-[220px] items-center gap-3">
        <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-[#0a0d12]/5">
          {icon}
        </span>
        <span className="text-[14.5px] font-medium">{label}</span>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {formats.map((f) =>
          f.href ? (
            <a
              key={f.label}
              href={f.href}
              onClick={f.onClick}
              className="inline-flex items-center gap-1.5 rounded-lg border border-[#0a0d12]/12 bg-white px-3 py-1.5 text-[13px] font-medium transition-colors hover:border-[#0a0d12]/30 hover:bg-[#0a0d12]/5"
            >
              {f.label}
            </a>
          ) : (
            <span
              key={f.label}
              aria-disabled="true"
              className="inline-flex cursor-not-allowed items-center gap-1.5 rounded-lg border border-[#0a0d12]/8 bg-[#0a0d12]/5 px-3 py-1.5 text-[13px] text-[#0a0d12]/40"
              title={unavailable}
            >
              {f.label}
            </span>
          ),
        )}
      </div>
    </div>
  );
}

// Ten desktop artifacts are expected per release (two Mac,
// two Windows, six Linux). If any are missing, surface the GitHub
// fallback link so users on an orphaned row have a way out.
const EXPECTED_ASSET_COUNT = 10;

function isFallbackNeeded(assets: DownloadAssets): boolean {
  return Object.values(assets).filter(Boolean).length < EXPECTED_ASSET_COUNT;
}
