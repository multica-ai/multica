"use client";

// Custom "Open in app" banner — iOS PWA fallback. iOS Safari does not honour
// `manifest.handle_links`, and Smart App Banners require a real App Store app.
// Until either lands (see JEH-1039 / JEH-1042), this is the only way to nudge
// mobile-web users into the installed PWA. On Android Chrome the manifest's
// `handle_links: "preferred"` is what actually captures the click — this banner
// is the surface that produces the click.

import { useEffect, useState } from "react";
import { X } from "lucide-react";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  readDismissedAt,
  shouldShowOpenInAppBanner,
  writeDismissedAt,
} from "./cerebro-open-in-app-banner-logic";

const STORAGE_KEY_PREFIX = "multica:open_in_app:dismissed_at";
const DEFAULT_ICON = "/icons/icon-192.png";

export interface CerebroOpenInAppBannerProps {
  /** Heading line. Defaults to the app name. */
  title?: string;
  /** Subline shown under the title. */
  description?: string;
  /** Square icon shown on the left. */
  iconSrc?: string;
  /** Label on the call-to-action. */
  openLabel?: string;
  /** Aria label for the close button. */
  closeLabel?: string;
  /** Whether to show a close (X) button. Defaults to true. */
  closeable?: boolean;
  /**
   * Where the "Open" button links. Defaults to the current pathname + search.
   * Must be within the PWA's `scope` for Android Chrome to capture it.
   */
  targetUrl?: string;
  /**
   * Days to suppress the banner after the user dismisses it. 0 = show again on
   * the next visit. Defaults to 7.
   */
  dismissForDays?: number;
  /**
   * Suffix on the localStorage key. Use this to render multiple banners on
   * different pages without their dismiss states colliding.
   */
  storageKey?: string;
  className?: string;
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

export function CerebroOpenInAppBanner({
  title = "Multica",
  description = "Open this page in the app",
  iconSrc = DEFAULT_ICON,
  openLabel = "Open",
  closeLabel = "Dismiss",
  closeable = true,
  targetUrl,
  dismissForDays = 7,
  storageKey,
  className,
}: CerebroOpenInAppBannerProps) {
  const fullStorageKey = storageKey
    ? `${STORAGE_KEY_PREFIX}:${storageKey}`
    : STORAGE_KEY_PREFIX;
  // Render nothing on the server / before hydration so the banner can never
  // flash for desktop users or already-installed-PWA users.
  const [visible, setVisible] = useState(false);
  const [href, setHref] = useState(targetUrl ?? "/");

  useEffect(() => {
    const storage =
      typeof window !== "undefined" ? window.localStorage : null;
    const ua =
      typeof navigator !== "undefined" ? navigator.userAgent : "";
    const next = shouldShowOpenInAppBanner({
      ua,
      isStandalone: detectStandalone(),
      dismissedAt: readDismissedAt(storage, fullStorageKey),
      dismissForDays,
    });
    setVisible(next);
    if (!targetUrl && typeof window !== "undefined") {
      setHref(window.location.pathname + window.location.search);
    } else if (targetUrl) {
      setHref(targetUrl);
    }
  }, [fullStorageKey, dismissForDays, targetUrl]);

  if (!visible) return null;

  const handleDismiss = () => {
    const storage =
      typeof window !== "undefined" ? window.localStorage : null;
    writeDismissedAt(storage, fullStorageKey, Date.now());
    setVisible(false);
  };

  return (
    <div
      role="region"
      aria-label="Open in app"
      data-testid="open-in-app-banner"
      className={cn(
        // Fixed at top so the banner never adds CLS to the page underneath.
        // Workspace shells already paint their own toolbar below this band.
        "fixed inset-x-0 top-0 z-50",
        "flex items-center gap-3 px-3 py-2",
        "border-b border-border bg-background/95 backdrop-blur shadow-sm",
        // Respect the iOS notch / home-indicator inset.
        "pt-[max(0.5rem,env(safe-area-inset-top))]",
        className,
      )}
    >
      <img
        src={iconSrc}
        alt=""
        aria-hidden="true"
        width={32}
        height={32}
        className="size-8 flex-shrink-0 rounded-md"
      />
      <div className="min-w-0 flex-1 text-sm">
        <div className="truncate font-medium text-foreground">{title}</div>
        <div className="truncate text-xs text-muted-foreground">
          {description}
        </div>
      </div>
      <a
        href={href}
        data-testid="open-in-app-banner-open"
        className={cn(
          buttonVariants({ size: "sm" }),
          "flex-shrink-0",
        )}
      >
        {openLabel}
      </a>
      {closeable && (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={closeLabel}
          data-testid="open-in-app-banner-dismiss"
          onClick={handleDismiss}
          className="flex-shrink-0"
        >
          <X className="size-4" aria-hidden="true" />
        </Button>
      )}
    </div>
  );
}
