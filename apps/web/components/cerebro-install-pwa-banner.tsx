"use client";

// FIR-2042: mobile-web "Install Multica" banner. Replaces the old
// "Open in app" banner — that one assumed the PWA was already installed and
// only deep-linked into it. This one nudges + guides the actual install on
// both platforms:
//   - Android Chrome: captures `beforeinstallprompt` and fires the native
//     install dialog from the "Install" button. If the event is unavailable
//     (criteria not yet met / non-Chromium), it falls back to menu guidance.
//   - iOS Safari: there is no programmatic install, so it shows the manual
//     Share → "Add to Home Screen" steps.
// Already-installed (standalone) sessions never see it. Gated behind the
// cerebro_pwa_install_banner flag.

import { useEffect, useState } from "react";
import { Share, X } from "lucide-react";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  detectMobilePlatform,
  readDismissedAt,
  shouldShowInstallBanner,
  writeDismissedAt,
  type MobilePlatform,
} from "./cerebro-install-pwa-banner-logic";

const STORAGE_KEY = "multica:install_pwa:dismissed_at";
const ICON_SRC = "/icons/icon-192.png";
const DISMISS_FOR_DAYS = 7;

// Minimal shape of the Chromium-only `beforeinstallprompt` event. Not in the
// DOM lib typings, so declared locally.
interface BeforeInstallPromptEvent extends Event {
  readonly platforms: string[];
  prompt: () => Promise<void>;
  readonly userChoice: Promise<{
    outcome: "accepted" | "dismissed";
    platform: string;
  }>;
}

function detectStandalone(): boolean {
  if (typeof window === "undefined") return false;
  // iOS Safari home-screen launch.
  const navAny = window.navigator as Navigator & { standalone?: boolean };
  if (navAny.standalone === true) return true;
  if (typeof window.matchMedia === "function") {
    return window.matchMedia("(display-mode: standalone)").matches;
  }
  return false;
}

export function CerebroInstallPwaBanner() {
  const enabled = useFeatureFlag("cerebro_pwa_install_banner");

  // Render nothing on the server / before hydration so the banner can never
  // flash for desktop users or already-installed-PWA users.
  const [visible, setVisible] = useState(false);
  const [platform, setPlatform] = useState<MobilePlatform>("other");
  const [deferredPrompt, setDeferredPrompt] =
    useState<BeforeInstallPromptEvent | null>(null);
  const [guideOpen, setGuideOpen] = useState(false);

  useEffect(() => {
    if (!enabled) return;
    const storage =
      typeof window !== "undefined" ? window.localStorage : null;
    const nav = typeof navigator !== "undefined" ? navigator : undefined;
    const detected = detectMobilePlatform({
      ua: nav?.userAgent ?? "",
      maxTouchPoints: nav?.maxTouchPoints ?? 0,
      platform: nav?.platform ?? "",
    });
    setPlatform(detected);
    setVisible(
      shouldShowInstallBanner({
        platform: detected,
        isStandalone: detectStandalone(),
        dismissedAt: readDismissedAt(storage, STORAGE_KEY),
        dismissForDays: DISMISS_FOR_DAYS,
      }),
    );

    const onBeforeInstallPrompt = (event: Event) => {
      // Stop Chrome's default mini-infobar so our banner is the only surface.
      event.preventDefault();
      setDeferredPrompt(event as BeforeInstallPromptEvent);
    };
    const onAppInstalled = () => {
      writeDismissedAt(storage, STORAGE_KEY, Date.now());
      setDeferredPrompt(null);
      setVisible(false);
    };
    window.addEventListener("beforeinstallprompt", onBeforeInstallPrompt);
    window.addEventListener("appinstalled", onAppInstalled);
    return () => {
      window.removeEventListener("beforeinstallprompt", onBeforeInstallPrompt);
      window.removeEventListener("appinstalled", onAppInstalled);
    };
  }, [enabled]);

  if (!enabled || !visible) return null;

  const dismiss = () => {
    const storage =
      typeof window !== "undefined" ? window.localStorage : null;
    writeDismissedAt(storage, STORAGE_KEY, Date.now());
    setVisible(false);
  };

  const triggerNativeInstall = async () => {
    if (!deferredPrompt) return;
    await deferredPrompt.prompt();
    const { outcome } = await deferredPrompt.userChoice;
    // The event can only be used once. Drop it either way; `appinstalled`
    // hides the banner on accept, and a dismiss collapses it for a week.
    setDeferredPrompt(null);
    if (outcome === "dismissed") dismiss();
    else setVisible(false);
  };

  const canNativeInstall = platform === "android" && deferredPrompt !== null;

  return (
    <div
      role="region"
      aria-label="Install Multica"
      data-testid="install-pwa-banner"
      className={cn(
        // Fixed at top so the banner never adds CLS to the page underneath.
        "fixed inset-x-0 top-0 z-50",
        "border-b border-border bg-background/95 backdrop-blur shadow-sm",
        // Respect the iOS notch / home-indicator inset.
        "pt-[max(0.5rem,env(safe-area-inset-top))]",
      )}
    >
      <div className="flex items-center gap-3 px-3 pb-2">
        <img
          src={ICON_SRC}
          alt=""
          aria-hidden="true"
          width={32}
          height={32}
          className="size-8 flex-shrink-0 rounded-md"
        />
        <div className="min-w-0 flex-1 text-sm">
          <div className="truncate font-medium text-foreground">
            Install Multica
          </div>
          <div className="truncate text-xs text-muted-foreground">
            Add it to your home screen for a full-screen, app-like experience.
          </div>
        </div>
        {canNativeInstall ? (
          <button
            type="button"
            data-testid="install-pwa-banner-install"
            onClick={triggerNativeInstall}
            className={cn(buttonVariants({ size: "sm" }), "flex-shrink-0")}
          >
            Install
          </button>
        ) : (
          <button
            type="button"
            data-testid="install-pwa-banner-how"
            aria-expanded={guideOpen}
            onClick={() => setGuideOpen((open) => !open)}
            className={cn(buttonVariants({ size: "sm" }), "flex-shrink-0")}
          >
            How to install
          </button>
        )}
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label="Dismiss"
          data-testid="install-pwa-banner-dismiss"
          onClick={dismiss}
          className="flex-shrink-0"
        >
          <X className="size-4" aria-hidden="true" />
        </Button>
      </div>

      {guideOpen && !canNativeInstall && (
        <div
          data-testid="install-pwa-banner-guide"
          className="border-t border-border px-3 py-2 text-xs text-muted-foreground"
        >
          {platform === "ios" ? (
            <ol className="list-decimal space-y-1 pl-4">
              <li>
                Tap the Share button{" "}
                <Share
                  className="inline size-3.5 align-text-bottom"
                  aria-hidden="true"
                />{" "}
                in the Safari toolbar.
              </li>
              <li>
                Scroll down and choose <strong>Add to Home Screen</strong>.
              </li>
              <li>
                Tap <strong>Add</strong> in the top-right corner.
              </li>
            </ol>
          ) : (
            <ol className="list-decimal space-y-1 pl-4">
              <li>
                Open the browser menu <strong>⋮</strong> in the top-right
                corner.
              </li>
              <li>
                Tap <strong>Install app</strong> (or{" "}
                <strong>Add to Home screen</strong>).
              </li>
              <li>Confirm to add the Multica icon to your home screen.</li>
            </ol>
          )}
        </div>
      )}
    </div>
  );
}
