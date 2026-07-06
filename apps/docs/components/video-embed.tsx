"use client";

import { useState } from "react";
import { Play } from "lucide-react";

/**
 * VideoEmbed — provider-agnostic, click-to-load video embed for docs MDX.
 *
 * Renders a lightweight facade (no third-party iframe on first paint) and only
 * mounts the real player after a user click, so the docs first paint never
 * pays for an external player or its trackers. `provider` is abstracted so a
 * future English-docs YouTube embed is a one-line MDX change, not a second
 * component.
 *
 * Usage in MDX (registered in the docs MDX components map):
 *   <VideoEmbed provider="bilibili" id="BV1cv7Y6gEg7" title="Multica 介绍视频" />
 */

type Provider = "bilibili" | "youtube";

interface ProviderConfig {
  /** Embeddable player URL. Autoplay is only requested after a user gesture. */
  embedUrl: (id: string, autoplay: boolean) => string;
  /** Canonical watch page — the load-failure / slow-network fallback link. */
  watchUrl: (id: string) => string;
  /** Human label for the fallback link ("在 Bilibili 观看"). */
  siteName: string;
  /** Validates the id shape so a typo renders a notice, not a broken frame. */
  isValidId: (id: string) => boolean;
}

const PROVIDERS: Record<Provider, ProviderConfig> = {
  bilibili: {
    embedUrl: (id, autoplay) =>
      `https://player.bilibili.com/player.html?bvid=${id}&autoplay=${autoplay ? 1 : 0}&high_quality=1&danmaku=0`,
    watchUrl: (id) => `https://www.bilibili.com/video/${id}/`,
    siteName: "Bilibili",
    isValidId: (id) => /^BV[0-9A-Za-z]+$/.test(id),
  },
  // Reserved for a future English-docs YouTube embed. Not wired into any page
  // yet, but kept here so the second provider is config, not a new component.
  youtube: {
    embedUrl: (id, autoplay) =>
      `https://www.youtube-nocookie.com/embed/${id}?autoplay=${autoplay ? 1 : 0}&rel=0`,
    watchUrl: (id) => `https://www.youtube.com/watch?v=${id}`,
    siteName: "YouTube",
    isValidId: (id) => /^[0-9A-Za-z_-]{11}$/.test(id),
  },
};

export function VideoEmbed({
  provider = "bilibili",
  id,
  title,
}: {
  provider?: Provider;
  id: string;
  title?: string;
}) {
  const [active, setActive] = useState(false);
  const config = PROVIDERS[provider];

  // Bad / missing id → a calm inline notice, never a broken or blank iframe.
  if (!config || !id || !config.isValidId(id)) {
    return (
      <div className="not-prose my-7 rounded-lg border border-border bg-muted/30 p-4 text-sm text-muted-foreground">
        视频暂时无法加载{title ? `：${title}` : ""}。
      </div>
    );
  }

  const watchUrl = config.watchUrl(id);
  const label = title ?? "观看视频";

  return (
    <figure className="not-prose my-7">
      <div className="relative aspect-video w-full overflow-hidden rounded-lg border border-border bg-muted/40">
        {active ? (
          <iframe
            src={config.embedUrl(id, true)}
            title={label}
            loading="lazy"
            allow="autoplay; fullscreen; encrypted-media; picture-in-picture"
            allowFullScreen
            className="absolute inset-0 size-full"
          />
        ) : (
          <button
            type="button"
            onClick={() => setActive(true)}
            aria-label={`播放：${label}`}
            className="group absolute inset-0 flex size-full flex-col items-center justify-center gap-3 bg-gradient-to-b from-muted/20 to-muted/60 transition-colors hover:from-muted/30 hover:to-muted/70"
          >
            <span className="flex size-16 items-center justify-center rounded-full bg-[var(--primary)] text-[var(--primary-foreground)] shadow-lg transition-transform group-hover:scale-105">
              <Play className="size-7 translate-x-0.5 fill-current" />
            </span>
            <span className="px-6 text-center text-sm font-medium text-foreground">
              {label}
            </span>
          </button>
        )}
      </div>
      <figcaption className="mt-2 text-xs text-muted-foreground">
        加载缓慢或无法播放？
        <a
          href={watchUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="underline underline-offset-2 hover:text-foreground"
        >
          在 {config.siteName} 观看
        </a>
      </figcaption>
    </figure>
  );
}
