"use client";

import Image from "next/image";
import Link from "next/link";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { captureDownloadIntent } from "@multica/core/analytics";
import { GitHubMark, githubUrl } from "./shared";
import { useLocale, locales, localeLabels } from "../i18n";

export function LandingFooter() {
  const { t, locale, setLocale } = useLocale();
  const user = useAuthStore((s) => s.user);
  const groups = Object.values(t.footer.groups);
  const icpRecordUrl = "https://beian.miit.gov.cn/";
  const policeRecordUrl = "https://beian.mps.gov.cn/#/query/webSearch";

  return (
    <>
      <footer className="bg-[#0a0d12] text-white">
        <div className="mx-auto max-w-[1320px] px-4 sm:px-6 lg:px-8">
          {/* Top: CTA + link columns */}
          <div className="flex flex-col gap-12 py-16 sm:py-20 lg:flex-row lg:gap-20">
            {/* Left — newsletter / CTA */}
            <div className="lg:w-[340px] lg:shrink-0">
              <Link href="#product" className="flex items-center gap-3">
                <MulticaIcon className="size-5 text-white" noSpin />
                <span className="text-[18px] font-semibold tracking-[0.04em] lowercase">
                  multica
                </span>
              </Link>
              <p className="mt-4 max-w-[300px] text-[14px] leading-[1.7] text-white/50 sm:text-[15px]">
                {t.footer.tagline}
              </p>
              <div className="mt-4 flex items-center gap-3">
                <Link
                  href={githubUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="text-white/40 transition-colors hover:text-white"
                >
                  <GitHubMark className="size-4" />
                </Link>
              </div>
              <div className="mt-6">
                <Link
                  href={user ? "/" : "/login"}
                  className="inline-flex items-center justify-center rounded-[11px] bg-white px-5 py-2.5 text-[13px] font-semibold text-[#0a0d12] transition-colors hover:bg-white/88"
                >
                  {user ? t.header.dashboard : t.footer.cta}
                </Link>
              </div>
            </div>

            {/* Right — link columns */}
            <div className="grid flex-1 grid-cols-2 gap-8 sm:grid-cols-4">
              {groups.map((group) => (
                <div key={group.label}>
                  <h4 className="text-[12px] font-semibold uppercase tracking-[0.1em] text-white/40">
                    {group.label}
                  </h4>
                  <ul className="mt-4 flex flex-col gap-2.5">
                    {group.links.map((link) => (
                      <li key={link.label}>
                        <Link
                          href={link.href}
                          {...(link.href.startsWith("http")
                            ? { target: "_blank", rel: "noreferrer" }
                            : {})}
                          onClick={
                            link.href === "/download"
                              ? () => captureDownloadIntent("landing_footer")
                              : undefined
                          }
                          className="text-[14px] text-white/50 transition-colors hover:text-white"
                        >
                          {link.label}
                        </Link>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </div>
        </div>
      </footer>

      <div className="border-t border-[#0a0d12]/10 bg-white text-[#0a0d12]">
        <div className="mx-auto flex max-w-[1320px] flex-col gap-3 px-4 py-4 text-[13px] sm:flex-row sm:items-center sm:justify-between sm:px-6 lg:px-8">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <span className="flex items-center gap-2 text-[#0a0d12]/78">
              <MulticaIcon className="size-4" noSpin />
              <span className="text-[14px] font-semibold tracking-[0.04em] lowercase">
                multica
              </span>
            </span>
            <span className="text-[#0a0d12]/70">
              {t.footer.copyright.replace(
                "{year}",
                String(new Date().getFullYear()),
              )}
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-[#0a0d12]/78">
            <Link
              href={icpRecordUrl}
              target="_blank"
              rel="noreferrer"
              className="hover:underline"
            >
              沪ICP备2023033672号-1
            </Link>
            <Link
              href={policeRecordUrl}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 hover:underline"
            >
              <Image
                src="/images/gongan-beian.png"
                alt=""
                aria-hidden="true"
                width={16}
                height={16}
                className="size-4 shrink-0"
              />
              沪公网安备31011302007480号
            </Link>
          </div>

          <div className="flex items-center">
            {locales.map((l, i) => (
              <button
                key={l}
                onClick={() => setLocale(l)}
                className={cn(
                  "px-1.5 py-1 text-[12px] font-medium transition-colors",
                  l === locale
                    ? "text-[#0a0d12]"
                    : "text-[#0a0d12]/42 hover:text-[#0a0d12]/70",
                  i > 0 && "border-l border-[#0a0d12]/16",
                )}
              >
                {localeLabels[l]}
              </button>
            ))}
          </div>
        </div>
      </div>
    </>
  );
}
