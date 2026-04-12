"use client";

import { useCallback, useState } from "react";
import Image from "next/image";
import Link from "next/link";
import { useAuthStore } from "@multica/core/auth";
import { useLocale } from "../i18n";
import {
  ClaudeCodeLogo,
  CodexLogo,
  OpenClawLogo,
  OpenCodeLogo,
  GitHubMark,
  githubUrl,
  heroButtonClassName,
} from "./shared";

export function LandingHero() {
  const { t } = useLocale();
  const user = useAuthStore((s) => s.user);

  return (
    <div className="relative min-h-full overflow-hidden bg-[#05070b] text-white">
      <LandingBackdrop />

      <main className="relative z-10">
        <section
          id="product"
          className="mx-auto max-w-[1320px] px-4 pb-16 pt-28 sm:px-6 sm:pt-32 lg:px-8 lg:pb-24 lg:pt-36"
        >
          <div className="mx-auto max-w-[1120px] text-center">
            <h1 className="font-[family-name:var(--font-serif)] text-[3.65rem] leading-[0.93] tracking-[-0.038em] text-white drop-shadow-[0_10px_34px_rgba(0,0,0,0.32)] sm:text-[4.85rem] lg:text-[6.4rem]">
              {t.hero.headlineLine1}
              <br />
              {t.hero.headlineLine2}
            </h1>

            <p className="mx-auto mt-7 max-w-[820px] text-[15px] leading-7 text-white/84 sm:text-[17px]">
              {t.hero.subheading}
            </p>

            <div className="mt-8 flex flex-wrap items-center justify-center gap-3">
              <Link href={user ? "/issues" : "/login"} className={heroButtonClassName("solid")}>
                {user ? t.header.dashboard : t.hero.cta}
              </Link>
              <Link
                href={githubUrl}
                target="_blank"
                rel="noreferrer"
                className={heroButtonClassName("ghost")}
              >
                <GitHubMark className="size-4" />
                GitHub
              </Link>
            </div>

            <InstallCommand />
          </div>

          <div className="mt-10 flex items-center justify-center gap-8">
            <span className="text-[15px] text-white/50">
              {t.hero.worksWith}
            </span>
            <div className="flex items-center gap-6">
              <div className="flex items-center gap-2.5 text-white/80">
                <ClaudeCodeLogo className="size-5" />
                <span className="text-[15px] font-medium">Claude Code</span>
              </div>
              <div className="flex items-center gap-2.5 text-white/80">
                <CodexLogo className="size-5" />
                <span className="text-[15px] font-medium">Codex</span>
              </div>
              <div className="flex items-center gap-2.5 text-white/80">
                <OpenClawLogo className="size-5" />
                <span className="text-[15px] font-medium">OpenClaw</span>
              </div>
              <div className="flex items-center gap-2.5 text-white/80">
                <OpenCodeLogo className="size-5" />
                <span className="text-[15px] font-medium">OpenCode</span>
              </div>
            </div>
          </div>

          <div id="preview" className="mt-10 sm:mt-12">
            <ProductImage alt={t.hero.imageAlt} />
          </div>
        </section>
      </main>
    </div>
  );
}

const INSTALL_COMMAND =
  "curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash";

function InstallCommand() {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(INSTALL_COMMAND);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // ignore
    }
  }, []);

  return (
    <div className="mx-auto mt-6 max-w-fit">
      <button
        type="button"
        onClick={handleCopy}
        className="group flex items-center gap-3 rounded-lg border border-white/10 bg-white/5 px-4 py-2.5 font-mono text-[13px] text-white/70 backdrop-blur-sm transition-colors hover:border-white/20 hover:bg-white/8 hover:text-white/90"
      >
        <span className="text-white/40">$</span>
        <span className="select-all">{INSTALL_COMMAND}</span>
        <span className="ml-1 flex size-5 shrink-0 items-center justify-center text-white/40 transition-colors group-hover:text-white/70">
          {copied ? (
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              className="size-3.5 text-green-400"
            >
              <polyline points="20 6 9 17 4 12" />
            </svg>
          ) : (
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              className="size-3.5"
            >
              <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
              <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
            </svg>
          )}
        </span>
      </button>
    </div>
  );
}

function LandingBackdrop() {
  return (
    <div className="pointer-events-none absolute inset-0">
      <Image
        src="/images/landing-bg.jpg"
        alt=""
        fill
        priority
        className="object-cover object-center"
      />
    </div>
  );
}

function ProductImage({ alt }: { alt: string }) {
  return (
    <div>
      <div className="relative overflow-hidden border border-white/14">
        <Image
          src="/images/landing-hero.png"
          alt={alt}
          width={3532}
          height={2382}
          className="block h-auto w-full"
          sizes="(max-width: 1320px) 100vw, 1320px"
          quality={85}
        />
      </div>
    </div>
  );
}
