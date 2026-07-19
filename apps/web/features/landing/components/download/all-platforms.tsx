import Link from "next/link";
import { useLocale } from "../../i18n";
import type { DownloadAssets } from "../../utils/parse-release-assets";
import { AppleIcon, LinuxIcon, WindowsIcon } from "./os-icons";

interface Props {
  assets: DownloadAssets;
  /** Link to GitHub releases page, used when individual asset URLs
   *  couldn't be resolved (API down / parse failure). */
  fallbackHref: string;
}

/**
 * Full matrix of platform + arch + format links. Always visible
 * regardless of which platform the Hero resolved to — lets power
 * users grab any build directly.
 */
export function AllPlatforms({
  assets,
  fallbackHref,
}: Props) {
  const { t } = useLocale();
  const d = t.download.allPlatforms;

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
            label={d.macArm64Label}
            formats={[
              {
                label: d.formatDmg,
                href: assets.macArm64Dmg,
              },
              {
                label: d.formatZip,
                href: assets.macArm64Zip,
              },
            ]}
            unavailable={d.unavailable}
          />
          <Row
            icon={<AppleIcon className="text-[#0a0d12]" />}
            label={d.macX64Label}
            formats={[
              {
                label: d.formatDmg,
                href: assets.macX64Dmg,
              },
              {
                label: d.formatZip,
                href: assets.macX64Zip,
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
              },
              {
                label: d.formatDeb,
                href: assets.linuxAmd64Deb,
              },
              {
                label: d.formatRpm,
                href: assets.linuxAmd64Rpm,
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
              },
              {
                label: d.formatDeb,
                href: assets.linuxArm64Deb,
              },
              {
                label: d.formatRpm,
                href: assets.linuxArm64Rpm,
              },
            ]}
            unavailable={d.unavailable}
            isLast
          />
        </div>

        {isFallbackNeeded(assets) ? (
          <p className="mt-6 text-[13px] text-[#0a0d12]/60">
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

// Twelve desktop artifacts are expected per release (four Mac,
// two Windows, six Linux). If any are missing, surface the GitHub
// fallback link so users on an orphaned row have a way out.
const EXPECTED_ASSET_COUNT = 12;

function isFallbackNeeded(assets: DownloadAssets): boolean {
  return Object.values(assets).filter(Boolean).length < EXPECTED_ASSET_COUNT;
}
