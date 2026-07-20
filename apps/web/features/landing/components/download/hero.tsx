import Link from "next/link";
import { ArrowRight, Download } from "lucide-react";
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
}: Props) {
  const { t } = useLocale();
  const d = t.download.hero;

  const content = resolveContent(detected, assets, versionUnavailable, d);

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

interface HeroContent {
  title: string;
  sub: string;
  primary?: {
    href: string;
    label: string;
    disabled: boolean;
  };
  alt?: { href: string; label: string };
  hint?: string;
}

type HeroDict = ReturnType<typeof useLocale>["t"]["download"]["hero"];

export function resolveContent(
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
    // "non-confident" as arm64 + point Intel users to the matrix below.
    if (detected.arch === "x64" && detected.archConfident) {
      const dmg = assets.macX64Dmg;
      const zip = assets.macX64Zip;
      return {
        title: d.macIntel.title,
        sub: d.macIntel.sub,
        primary: dmg
          ? {
              href: dmg,
              label: d.macIntel.primary,
              disabled: false,
            }
          : versionUnavailable
            ? { href: "#", label: d.macIntel.primary, disabled: true }
            : undefined,
        alt: zip
          ? {
              href: zip,
              label: d.macIntel.altZip,
            }
          : undefined,
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
          }
        : versionUnavailable
          ? { href: "#", label: d.macArm64.primary, disabled: true }
          : undefined,
      alt: zip
        ? {
            href: zip,
            label: d.macArm64.altZip,
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
  children,
}: {
  href: string;
  disabled: boolean;
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
    <a href={href} className={heroButtonClassName("solid")}>
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
