"use client";

import Link from "@/features/landing/components/landing-link";
import { ChevronDown, ChevronRight } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useLocale } from "../../i18n";
import { focusRing } from "../../site/primitives";
import { formatSize, osOf, platformOf, type PrimaryDownloadKey } from "../../utils/download-target";
import type { DownloadAssets, DownloadSizes } from "../../utils/parse-release-assets";
import { DownloadLink } from "./download-link";
import { OSIcon, OS_NAMES } from "./os-icons";

/** One installer per platform, in menu order. ARM builds and other formats live on /download. */
const MENU_KEYS: PrimaryDownloadKey[] = ["macArm64Dmg", "macX64Dmg", "winX64Exe", "linuxAmd64AppImage"];

const FORMAT: Record<PrimaryDownloadKey, string> = {
  macArm64Dmg: ".dmg",
  macX64Dmg: ".dmg",
  winX64Exe: ".exe",
  winArm64Exe: ".exe",
  linuxAmd64AppImage: ".AppImage",
  linuxArm64AppImage: ".AppImage",
};

const ITEM = "gap-3 rounded-site-inner px-2.5 py-2 text-site-ink focus:bg-site-hover focus:text-site-ink";

/**
 * Under the homepage download button: the other platforms' installers,
 * one click each, and a way to every build.
 */
export function OtherDownloads({
  primary,
  assets,
  sizes,
  version,
}: {
  primary: PrimaryDownloadKey;
  assets: DownloadAssets;
  sizes: DownloadSizes;
  version: string | null;
}) {
  const { t } = useLocale();
  const items = MENU_KEYS.filter((key) => key !== primary && assets[key]);
  if (items.length === 0) return null;

  // The visitor's own OS reads as its other architecture, the rest by name: "Intel · Windows · Linux".
  const names = [
    ...new Set(
      items.map((key) => {
        if (osOf(key) !== osOf(primary)) return OS_NAMES[osOf(key)];
        return t.download.platforms[platformOf(key)].split(" · ")[1]!;
      }),
    ),
  ];

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        className={cn(
          "group mt-3 inline-flex items-center gap-1 rounded-sm text-site-small text-site-muted transition-colors hover:text-site-ink data-popup-open:text-site-ink",
          focusRing,
        )}
      >
        {t.home.hero.alsoFor.replace("{platforms}", names.join(" · "))}
        <ChevronDown className="size-3.5 transition-transform duration-200 group-data-popup-open:rotate-180" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        sideOffset={8}
        className="landing-light w-72 rounded-site-card bg-site-surface p-1.5 text-site-ink shadow-site-menu ring-site-line"
      >
        {items.map((key) => {
          const [osName, arch] = t.download.platforms[platformOf(key)].split(" · ");
          const size = sizes[key];
          return (
            <DropdownMenuItem
              key={key}
              className={ITEM}
              render={<DownloadLink downloadKey={key} href={assets[key]!} handoff />}
            >
              <span className="flex size-8 shrink-0 items-center justify-center rounded-site-inner bg-site-sunken text-site-ink-secondary">
                <OSIcon os={osOf(key)} size={16} aria-hidden />
              </span>
              <span className="min-w-0">
                <span className="block text-site-small">
                  {osName} <span className="text-site-muted">{arch}</span>
                </span>
                <span className="block text-[0.8125rem] text-site-muted">
                  {FORMAT[key]}
                  {size ? ` · ${formatSize(size)}` : null}
                </span>
              </span>
            </DropdownMenuItem>
          );
        })}
        <DropdownMenuSeparator className="mx-1 my-1 bg-site-rule" />
        <DropdownMenuItem className={cn(ITEM, "py-1.5 text-site-small")} render={<Link href="/download" />}>
          <span className="flex-1">{t.download.allPlatforms.title}</span>
          {version ? <span className="text-site-muted">{version}</span> : null}
          <ChevronRight className="size-3.5 text-site-muted" aria-hidden />
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
