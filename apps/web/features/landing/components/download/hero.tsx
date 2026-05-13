import Link from "next/link";
import { ArrowRight, Download } from "lucide-react";
import {
  captureDownloadInitiated,
  type DownloadInitiatedPayload,
} from "@multica/core/analytics";
import { useLocale } from "../../i18n";
import type { DetectResult } from "../../utils/os-detect";
import type { DownloadAssets } from "../../utils/parse-release-assets";
import { heroButtonClassName } from "../shared";

interface Props {
  detected: DetectResult | null;
  assets: DownloadAssets;
  /** True when the GitHub API fetch failed; disables all CTAs and
   *  surfaces a "version unavailable" line. */
  versionUnavailable: boolean;
  /** Release tag (e.g. "v0.2.13"). Null when version lookup failed —
   *  in that case CTAs are already disabled, no tracking fires. */
  version: string | null;
}

/**
 * Top CTA section. Server-renders a generic "Choose your platform"
 * placeholder (SEO + flash-before-hydration), then swaps to a
 * platform-specific CTA once the client detection resolves.
 */
export function DownloadHero({
  detected,
  assets,
  versionUnavailable,
  version,
}: Props) {
  const { t } = useLocale();
  const d = t.download.hero;

  const content = resolveContent(detected, assets, versionUnavailable, d);

  // Fires download_initiated on primary CTA click. `primary_cta: true`
  // identifies the hero-recommended path; `matched_detect: true` is
  // always true here by construction (the primary is computed from
  // the detect result). All Platforms rows below emit with
  // matched_detect=false when the user overrides.
  const onPrimaryClick = (tracking: HeroTracking | undefined) => {
    if (!tracking || !version) return;
    captureDownloadInitiated({
      ...tracking,
      version,
      primary_cta: true,
      matched_detect: true,
    });
  };

  return (
    <section className="relative overflow-hidden bg-[#05070b] text-white">
      <BackdropGradient />
      <div className="relative z-10 mx-auto max-w-[1120px] px-4 pb-24 pt-32 text-center sm:px-6 sm:pt-40 lg:px-8 lg:pb-28">
        <h1 className="mx-auto max-w-[880px] font-[family-name:var(--font-serif)] text-[3rem] leading-[1.02] tracking-[-0.035em] drop-shadow-[0_10px_34px_rgba(0,0,0,0.32)] sm:text-[4rem] lg:text-[5rem]">
          {content.title}
        </h1>
        <p className="mx-auto mt-6 max-w-[620px] text-[15px] leading-7 text-white/84 sm:text-[17px]">
          {content.sub}
        </p>

        <div className="mt-10 flex flex-wrap items-center justify-center gap-3">
          {content.primary ? (
            <PrimaryCta
              href={content.primary.href}
              disabled={content.primary.disabled}
              onClick={() => onPrimaryClick(content.primary?.tracking)}
            >
              <Download className="size-4" aria-hidden />
              {content.primary.label}
              {!content.primary.disabled && (
                <ArrowRight className="size-4" aria-hidden />
              )}
            </PrimaryCta>
          ) : null}
          {content.alt ? (
            <Link
              href={content.alt.href}
              className={heroButtonClassName("ghost")}
              onClick={() => onPrimaryClick(content.alt?.tracking)}
            >
              {content.alt.label}
            </Link>
          ) : null}
        </div>

        {content.hint ? (
          <p className="mx-auto mt-5 max-w-[520px] text-[13px] text-white/64">
            {content.hint}
          </p>
        ) : null}

        {versionUnavailable ? (
          <p className="mx-auto mt-6 max-w-[520px] text-[12px] uppercase tracking-[0.14em] text-white/50">
            {t.download.footer.versionUnavailable}
          </p>
        ) : null}
      </div>
    </section>
  );
}

// ------------------------------------------------------------
// Content resolver — maps (detect, assets) → CTA props
// ------------------------------------------------------------

type HeroTracking = Pick<
  DownloadInitiatedPayload,
  "platform" | "arch" | "format"
>;

interface HeroContent {
  title: string;
  sub: string;
  primary?: {
    href: string;
    label: string;
    disabled: boolean;
    tracking?: HeroTracking;
  };
  alt?: { href: string; label: string; tracking?: HeroTracking };
  hint?: string;
}

type HeroDict = ReturnType<typeof useLocale>["t"]["download"]["hero"];

function resolveContent(
  detected: DetectResult | null,
  assets: DownloadAssets,
  versionUnavailable: boolean,
  d: HeroDict,
): HeroContent {
  // Before hydration resolves, render a neutral prompt. Same copy
  // also catches `os === "unknown"`.
  if (!detected || detected.os === "unknown") {
    return { title: d.unknown.title, sub: d.unknown.sub };
  }

  if (detected.os === "mac") {
    // Only Chromium high-entropy returns arch confidently. Safari
    // always reports Intel even on Apple Silicon, so we treat
    // "non-confident" as arm64 + add a small Intel disclaimer.
    if (detected.arch === "x64" && detected.archConfident) {
      return {
        title: d.macIntel.title,
        sub: d.macIntel.sub,
        primary: {
          href: "#cli",
          label: d.macIntel.disabledCta,
          disabled: true,
        },
        hint: d.macIntel.intelHint,
      };
    }
    const dmg = assets.macArm64Dmg;
    const zip = assets.macArm64Zip;
    return {
      title: d.macArm64.title,
      sub: d.macArm64.sub,
      primary: dmg
        ? {
            href: dmg,
            label: d.macArm64.primary,
            disabled: false,
            tracking: { platform: "mac", arch: "arm64", format: "dmg" },
          }
        : versionUnavailable
          ? { href: "#", label: d.macArm64.primary, disabled: true }
          : undefined,
      alt: zip
        ? {
            href: zip,
            label: d.macArm64.altZip,
            tracking: { platform: "mac", arch: "arm64", format: "zip" },
          }
        : undefined,
      hint: detected.archConfident ? undefined : d.safariMacHint,
    };
  }

  if (detected.os === "windows") {
    // Trust arch whenever the UA hints at it (even non-confident);
    // Windows-on-ARM can still run x64 via emulation so this is low
    // risk either way. Surface the arch-fallback hint when we're
    // guessing so users on uncommon setups know to scroll down.
    const isArm = detected.arch === "arm64";
    const copy = isArm ? d.winArm64 : d.winX64;
    const url = isArm ? assets.winArm64Exe : assets.winX64Exe;
    return {
      title: copy.title,
      sub: copy.sub,
      primary: url
        ? {
            href: url,
            label: copy.primary,
            disabled: false,
            tracking: {
              platform: "windows",
              arch: isArm ? "arm64" : "x64",
              format: "exe",
            },
          }
        : versionUnavailable
          ? { href: "#", label: copy.primary, disabled: true }
          : undefined,
      hint: detected.archConfident ? undefined : d.archFallbackHint,
    };
  }

  // Linux — same principle: trust the arm64 signal, surface a hint
  // when we're not confident. Linux ARM has no binary emulation so
  // the hint matters more here than on Windows.
  const isArmLinux = detected.arch === "arm64";
  const primaryUrl = isArmLinux
    ? assets.linuxArm64AppImage
    : assets.linuxAmd64AppImage;
  return {
    title: d.linux.title,
    sub: d.linux.sub,
    primary: primaryUrl
      ? {
          href: primaryUrl,
          label: d.linux.primary,
          disabled: false,
          tracking: {
            platform: "linux",
            arch: isArmLinux ? "arm64" : "x64",
            format: "appimage",
          },
        }
      : versionUnavailable
        ? { href: "#", label: d.linux.primary, disabled: true }
        : undefined,
    alt: { href: "#all-platforms", label: d.linux.altFormats },
    hint: detected.archConfident ? undefined : d.archFallbackHint,
  };
}

// ------------------------------------------------------------
// Pieces
// ------------------------------------------------------------

function PrimaryCta({
  href,
  disabled,
  onClick,
  children,
}: {
  href: string;
  disabled: boolean;
  onClick?: () => void;
  children: React.ReactNode;
}) {
  if (disabled) {
    return (
      <span
        aria-disabled="true"
        className="inline-flex cursor-not-allowed items-center justify-center gap-2 rounded-[12px] border border-white/15 bg-white/8 px-5 py-3 text-[14px] font-semibold text-white/60"
      >
        {children}
      </span>
    );
  }
  return (
    <a href={href} onClick={onClick} className={heroButtonClassName("solid")}>
      {children}
    </a>
  );
}

function BackdropGradient() {
  return (
    <div
      aria-hidden
      className="pointer-events-none absolute inset-0"
      style={{
        background:
          "radial-gradient(ellipse 70% 50% at 50% 0%, rgba(80,120,255,0.18), transparent 60%), radial-gradient(ellipse 50% 40% at 50% 80%, rgba(255,90,90,0.08), transparent 60%)",
      }}
    />
  );
}
