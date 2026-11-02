"use client";

import { DOWNLOAD_BASE_URL } from "@multica/core/deployment";

import { useEffect, useState } from "react";
import type { SupportedLocale } from "@multica/core/i18n";

// Internal release page: desktop installers + CLI artifacts from the internal
// /downloads source. Ports the functional core of the old marketing download
// page (OS detection, per-platform assets, CLI one-liner) without the landing
// chrome that Stage 1 removed.
const DOWNLOADS = DOWNLOAD_BASE_URL;
const CLI_INSTALL = `curl -fsSL ${DOWNLOADS}/install.sh | bash`;
const CLI_VERSIONED = (v: string, f: string) =>
  `${DOWNLOADS}/cli/v${v}/${f}`;

type OSKind = "windows" | "macos" | "linux" | null;

const COPY: Record<
  Exclude<SupportedLocale, "zh-Hant">,
  {
    title: string;
    subtitle: string;
    detected: Record<Exclude<OSKind, null>, string>;
    undetected: string;
    downloadFor: string;
    desktop: string;
    cli: string;
    cliCopy: string;
    copied: string;
    allAssets: string;
    desktopSection: string;
    desktopHint: string;
    cliSection: string;
    checksums: string;
    current: string;
    versionUnavailable: string;
  }
> = {
  en: {
    title: "Download",
    subtitle: "Desktop and CLI builds from the internal release channel.",
    detected: {
      windows: "Windows detected",
      macos: "macOS detected",
      linux: "Linux detected",
    },
    undetected: "Pick your platform below.",
    downloadFor: "Download for",
    desktop: "Desktop",
    cli: "CLI",
    cliCopy: "Copy",
    copied: "Copied",
    allAssets: "All assets",
    desktopSection: "Desktop app",
    desktopHint:
      "Windows installer (nsis). macOS and Linux desktop builds are not published yet.",
    cliSection: "CLI (macOS / Linux)",
    checksums: "checksums",
    current: "Current version",
    versionUnavailable: "Version info temporarily unavailable",
  },
  fr: {
    title: "Télécharger",
    subtitle: "Applications de bureau et CLI du canal de diffusion interne.",
    detected: { windows: "Windows détecté", macos: "macOS détecté", linux: "Linux détecté" },
    undetected: "Choisissez votre plateforme ci-dessous.",
    downloadFor: "Télécharger pour",
    desktop: "Application de bureau",
    cli: "CLI",
    cliCopy: "Copier",
    copied: "Copié",
    allAssets: "Tous les fichiers",
    desktopSection: "Application de bureau",
    desktopHint: "Installateur Windows (nsis). Les versions de bureau macOS et Linux ne sont pas encore publiées.",
    cliSection: "CLI (macOS / Linux)",
    checksums: "Sommes de contrôle",
    current: "Version actuelle",
    versionUnavailable: "Informations de version temporairement indisponibles",
  },
  "zh-Hans": {
    title: "下载",
    subtitle: "桌面端与 CLI 安装包均来自内部发布渠道。",
    detected: { windows: "检测到 Windows", macos: "检测到 macOS", linux: "检测到 Linux" },
    undetected: "请在下方选择你的平台。",
    downloadFor: "下载",
    desktop: "桌面端",
    cli: "CLI",
    cliCopy: "复制",
    copied: "已复制",
    allAssets: "全部产物",
    desktopSection: "桌面应用",
    desktopHint: "Windows 安装包（nsis）。macOS 与 Linux 桌面版尚未发布。",
    cliSection: "CLI（macOS / Linux）",
    checksums: "校验文件",
    current: "当前版本",
    versionUnavailable: "版本信息暂时不可用",
  },
  ja: {
    title: "ダウンロード",
    subtitle: "デスクトップ版と CLI は社内リリースチャネルから配布されます。",
    detected: { windows: "Windows を検出", macos: "macOS を検出", linux: "Linux を検出" },
    undetected: "下からプラットフォームを選択してください。",
    downloadFor: "ダウンロード",
    desktop: "デスクトップ",
    cli: "CLI",
    cliCopy: "コピー",
    copied: "コピーしました",
    allAssets: "すべてのアセット",
    desktopSection: "デスクトップアプリ",
    desktopHint:
      "Windows インストーラー（nsis）。macOS / Linux のデスクトップ版は未公開です。",
    cliSection: "CLI（macOS / Linux）",
    checksums: "チェックサム",
    current: "現在のバージョン",
    versionUnavailable: "バージョン情報は一時的に利用できません",
  },
  ko: {
    title: "다운로드",
    subtitle: "데스크톱과 CLI 빌드는 사내 릴리스 채널에서 배포됩니다.",
    detected: { windows: "Windows 감지됨", macos: "macOS 감지됨", linux: "Linux 감지됨" },
    undetected: "아래에서 플랫폼을 선택하세요.",
    downloadFor: "다운로드",
    desktop: "데스크톱",
    cli: "CLI",
    cliCopy: "복사",
    copied: "복사됨",
    allAssets: "모든 에셋",
    desktopSection: "데스크톱 앱",
    desktopHint: "Windows 설치 프로그램(nsis). macOS / Linux 데스크톱은 아직 미출시입니다.",
    cliSection: "CLI(macOS / Linux)",
    checksums: "체크섬",
    current: "현재 버전",
    versionUnavailable: "버전 정보를 일시적으로 사용할 수 없습니다",
  },
};

function detectOS(): OSKind {
  if (typeof navigator === "undefined") return null;
  const ua = navigator.userAgent.toLowerCase();
  if (ua.includes("win")) return "windows";
  if (ua.includes("mac")) return "macos";
  if (ua.includes("linux")) return "linux";
  return null;
}

type Asset = { label: string; href: string; note?: string };

export function DownloadClient({
  locale,
  desktopVersion,
  desktopPath,
}: {
  locale: SupportedLocale;
  desktopVersion: string | null;
  desktopPath: string | null;
}) {
  const copy = COPY[locale === "zh-Hant" ? "zh-Hans" : locale];
  const [os, setOs] = useState<OSKind>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    setOs(detectOS());
  }, []);

  const desktopAssets: Asset[] = desktopVersion
    ? [
        {
          label: `Windows x64 (.exe)`,
          href: desktopPath ?? `${DOWNLOADS}/desktop/`,
        },
      ]
    : [];

  const cliAssets: Asset[] = desktopVersion
    ? [
        { label: "macOS arm64", href: CLI_VERSIONED(desktopVersion, "multica-cli-" + desktopVersion + "-darwin-arm64.tar.gz") },
        { label: "macOS amd64", href: CLI_VERSIONED(desktopVersion, `multica-cli-${desktopVersion}-darwin-amd64.tar.gz`) },
        { label: "Linux arm64", href: CLI_VERSIONED(desktopVersion, `multica-cli-${desktopVersion}-linux-arm64.tar.gz`) },
        { label: "Linux amd64", href: CLI_VERSIONED(desktopVersion, `multica-cli-${desktopVersion}-linux-amd64.tar.gz`) },
      ]
    : [];

  const primary =
    os === "windows"
      ? desktopAssets[0]
      : os === "macos" || os === "linux"
        ? { label: copy.cli, href: "" }
        : null;

  async function copyInstall() {
    try {
      await navigator.clipboard.writeText(CLI_INSTALL);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* clipboard unavailable — the command stays selectable as text */
    }
  }

  return (
    <div className="flex min-h-svh flex-col items-center bg-background px-6 py-16 text-foreground">
      <div className="w-full max-w-2xl">
        <h1 className="text-title-lg font-medium tracking-tight">
          {copy.title}
        </h1>
        <p className="mt-2 text-body text-muted-foreground">{copy.subtitle}</p>

        {desktopVersion ? (
          <p className="mt-1 text-label text-muted-foreground">
            {copy.current}: {desktopVersion}
          </p>
        ) : (
          <p className="mt-1 text-label text-muted-foreground">
            {copy.versionUnavailable}
          </p>
        )}

        {/* Primary action: OS-aware */}
        <div className="mt-8 rounded-lg border border-border bg-card p-5">
          <p className="text-label text-muted-foreground">
            {os ? copy.detected[os] : copy.undetected}
          </p>
          {os === "windows" && desktopAssets[0] ? (
            <a
              href={desktopAssets[0].href}
              className="mt-3 inline-flex items-center justify-center rounded-md bg-primary px-5 py-2.5 text-body font-medium text-primary-foreground hover:bg-primary/90"
              download
            >
              {copy.downloadFor} Windows (.exe)
            </a>
          ) : (
            <div className="mt-3 flex flex-wrap items-center gap-3">
              <code className="rounded-md bg-muted px-3 py-2 text-label font-mono">
                {CLI_INSTALL}
              </code>
              <button
                type="button"
                onClick={copyInstall}
                className="rounded-md border border-border px-3 py-2 text-label hover:bg-muted"
              >
                {copied ? copy.copied : copy.cliCopy}
              </button>
            </div>
          )}
        </div>

        {/* Desktop section */}
        {desktopAssets.length > 0 && (
          <section className="mt-8">
            <h2 className="text-body font-medium">{copy.desktopSection}</h2>
            <ul className="mt-3 space-y-2">
              {desktopAssets.map((a) => (
                <li key={a.href} className="flex items-baseline gap-3">
                  <a
                    href={a.href}
                    className="text-body text-primary underline underline-offset-4"
                    download
                  >
                    {a.label}
                  </a>
                </li>
              ))}
            </ul>
            <p className="mt-2 text-label text-muted-foreground">
              {copy.desktopHint}
            </p>
          </section>
        )}

        {/* CLI section */}
        <section className="mt-8">
          <h2 className="text-body font-medium">{copy.cliSection}</h2>
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <code className="rounded-md bg-muted px-3 py-2 text-label font-mono">
              {CLI_INSTALL}
            </code>
            <button
              type="button"
              onClick={copyInstall}
              className="rounded-md border border-border px-3 py-2 text-label hover:bg-muted"
            >
              {copied ? copy.copied : copy.cliCopy}
            </button>
          </div>
          {cliAssets.length > 0 && (
            <ul className="mt-3 flex flex-wrap gap-x-5 gap-y-2">
              {cliAssets.map((a) => (
                <li key={a.href}>
                  <a
                    href={a.href}
                    className="text-label text-primary underline underline-offset-4"
                  >
                    {a.label}
                  </a>
                </li>
              ))}
              <li>
                <a
                  href={`${DOWNLOADS}/cli/v${desktopVersion}/checksums.txt`}
                  className="text-label text-muted-foreground underline underline-offset-4"
                >
                  {copy.checksums}
                </a>
              </li>
            </ul>
          )}
        </section>
      </div>
    </div>
  );
}
