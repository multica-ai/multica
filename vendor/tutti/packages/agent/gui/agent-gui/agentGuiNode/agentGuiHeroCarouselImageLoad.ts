import claudeVinylAssetUrl from "../../app/renderer/assets/icons/agent-vinyls/claude-vinyl.png";
import codexVinylAssetUrl from "../../app/renderer/assets/icons/agent-vinyls/codex-vinyl.png";
import cursorVinylAssetUrl from "../../app/renderer/assets/icons/agent-vinyls/cursor-vinyl.png";
import openclawVinylAssetUrl from "../../app/renderer/assets/icons/agent-vinyls/openclaw-vinyl.png";
import opencodeVinylAssetUrl from "../../app/renderer/assets/icons/agent-vinyls/opencode-vinyl.png";
import tuttiVinylAssetUrl from "../../app/renderer/assets/icons/agent-vinyls/tutti-vinyl.png";
import {
  agentGuiScheduler,
  type AgentGuiScheduledTask
} from "./agentGuiScheduler";
import type { AgentGUIAgentAvatarPresentation } from "./model/agentGuiAgentAvatarPresentation";
import { normalizeManagedAgentProvider } from "../../shared/managedAgentProviders";

const AGENT_VINYL_COVER_BY_PROVIDER: Readonly<Record<string, string>> = {
  "claude-code": claudeVinylAssetUrl,
  codex: codexVinylAssetUrl,
  cursor: cursorVinylAssetUrl,
  openclaw: openclawVinylAssetUrl,
  opencode: opencodeVinylAssetUrl,
  "tutti-agent": tuttiVinylAssetUrl
};

const BADGE_IMAGE_RETRY_DELAYS_MS = [150, 500] as const;

export interface AgentGuiHeroCarouselDecodedImages {
  badges: readonly (HTMLImageElement | null)[];
  covers: readonly (HTMLImageElement | null)[];
  icons: readonly (HTMLImageElement | null)[];
}

interface PendingImageLoad {
  cancel(): void;
  promise: Promise<HTMLImageElement | null>;
}

export class AgentGuiHeroCarouselImageLoad {
  readonly result: Promise<AgentGuiHeroCarouselDecodedImages>;
  private readonly pendingLoads = new Set<PendingImageLoad>();
  private canceled = false;

  constructor(items: readonly AgentGUIAgentAvatarPresentation[]) {
    this.result = Promise.all(
      items.map(async (item) => {
        const [icon, cover, badge] = await Promise.all([
          this.loadImage(item.iconUrl),
          this.loadImage(
            item.heroImageUrl?.trim() ||
              AGENT_VINYL_COVER_BY_PROVIDER[normalizeManagedAgentProvider(item.provider)] ||
              null
          ),
          this.loadImage(item.badge?.iconUrl ?? null, true)
        ]);
        return { badge, cover, icon };
      })
    ).then((entries) => ({
      badges: entries.map((entry) => entry.badge),
      covers: entries.map((entry) => entry.cover),
      icons: entries.map((entry) => entry.icon)
    }));
  }

  cancel(): void {
    if (this.canceled) {
      return;
    }
    this.canceled = true;
    for (const pending of this.pendingLoads) {
      pending.cancel();
    }
    this.pendingLoads.clear();
  }

  private loadImage(
    url: string | null,
    retryTransientFailure = false
  ): Promise<HTMLImageElement | null> {
    if (!url || this.canceled || typeof Image !== "function") {
      return Promise.resolve(null);
    }
    let settled = false;
    let image: HTMLImageElement | null = null;
    let retryIndex = 0;
    let retryTask: AgentGuiScheduledTask | null = null;
    let resolvePromise = (_value: HTMLImageElement | null): void => undefined;
    const pending: PendingImageLoad = {
      cancel: () => settle(null, true),
      promise: new Promise((resolve) => {
        resolvePromise = resolve;
      })
    };
    const settle = (
      value: HTMLImageElement | null,
      clearSource = false
    ): void => {
      if (settled) {
        return;
      }
      settled = true;
      if (retryTask !== null) {
        retryTask.cancel();
        retryTask = null;
      }
      if (image) {
        image.onload = null;
        image.onerror = null;
      }
      this.pendingLoads.delete(pending);
      if (clearSource && image) {
        image.src = "";
      }
      resolvePromise(value);
    };
    const settleDecoded = (loadedImage: HTMLImageElement): void => {
      let decode: Promise<void> | undefined;
      try {
        decode = loadedImage.decode?.();
      } catch {
        settle(loadedImage);
        return;
      }
      if (decode) {
        void decode
          .then(() => settle(loadedImage))
          .catch(() => settle(loadedImage));
        return;
      }
      settle(loadedImage);
    };
    const retryOrSettle = (failedImage: HTMLImageElement): void => {
      failedImage.onload = null;
      failedImage.onerror = null;
      if (
        retryTransientFailure &&
        retryIndex < BADGE_IMAGE_RETRY_DELAYS_MS.length &&
        !this.canceled
      ) {
        failedImage.src = "";
        const delay = BADGE_IMAGE_RETRY_DELAYS_MS[retryIndex]!;
        retryIndex += 1;
        retryTask = agentGuiScheduler.schedule(delay, () => {
          retryTask = null;
          startAttempt();
        });
        return;
      }
      settle(null);
    };
    const startAttempt = (): void => {
      if (settled || this.canceled) {
        settle(null, true);
        return;
      }
      const nextImage = new Image();
      image = nextImage;
      nextImage.crossOrigin = "anonymous";
      nextImage.decoding = "async";
      nextImage.loading = "eager";
      nextImage.setAttribute("fetchpriority", "high");
      let attemptFinished = false;
      const handleLoaded = (): void => {
        if (attemptFinished) {
          return;
        }
        attemptFinished = true;
        settleDecoded(nextImage);
      };
      const handleFailed = (): void => {
        if (attemptFinished) {
          return;
        }
        attemptFinished = true;
        retryOrSettle(nextImage);
      };
      nextImage.onload = handleLoaded;
      nextImage.onerror = handleFailed;
      nextImage.src = url;
      if (nextImage.complete) {
        if (nextImage.naturalWidth > 0) {
          handleLoaded();
        } else {
          handleFailed();
        }
      }
    };
    this.pendingLoads.add(pending);
    startAttempt();
    return pending.promise;
  }
}
