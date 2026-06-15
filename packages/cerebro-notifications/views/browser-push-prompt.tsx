"use client";

import { useCallback, useEffect, useState } from "react";
import { Bell, Loader2, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { subscribeThisDevice } from "./push-notifications-section";

// TECH-3548: a dismissible banner shown only in a browser (never the Electron
// desktop app) that invites the user to turn on push notifications. It asks for
// the OS permission inline and links to the full notification settings. Once
// the user enables, denies, or dismisses, it stays hidden.

const DISMISS_KEY = "cerebro_push_prompt_dismissed";

type Phase = "hidden" | "visible" | "working";

// Electron exposes `window.electron` via its preload script; the desktop app
// also carries "Electron" in its UA. Either signal means we're NOT in a plain
// browser and the prompt should never show.
function isPlainBrowser(): boolean {
  if (typeof window === "undefined") return false;
  if ((window as unknown as { electron?: unknown }).electron) return false;
  return !/electron/i.test(navigator.userAgent);
}

function supportsWebPush(): boolean {
  return (
    typeof window !== "undefined" &&
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window
  );
}

interface BrowserPushPromptProps {
  // Path to the notification settings tab in the current workspace, e.g.
  // `/acme/settings?tab=notifications`. Supplied by the host app so this shared
  // component stays free of routing specifics.
  settingsHref: string;
}

export function BrowserPushPrompt({ settingsHref }: BrowserPushPromptProps) {
  const promptEnabled = useFeatureFlag("cerebro_browser_push_prompt");
  const webPushEnabled = useFeatureFlag("cerebro_web_push");
  const [phase, setPhase] = useState<Phase>("hidden");
  const [publicKey, setPublicKey] = useState("");

  useEffect(() => {
    if (!promptEnabled || !webPushEnabled) return;
    if (!isPlainBrowser() || !supportsWebPush()) return;
    if (Notification.permission !== "default") return; // granted or denied → no prompt
    try {
      if (window.localStorage.getItem(DISMISS_KEY) === "1") return;
    } catch {
      // localStorage can throw in private mode — fall through and show.
    }

    let cancelled = false;
    void (async () => {
      try {
        const cfg = await api.getPushPublicKey();
        if (cancelled || !cfg.enabled || !cfg.publicKey) return;
        const reg = await navigator.serviceWorker.ready;
        const existing = await reg.pushManager.getSubscription();
        if (cancelled || existing) return; // already subscribed on this device
        setPublicKey(cfg.publicKey);
        setPhase("visible");
      } catch {
        // Network / SW hiccup — just don't show the prompt this time.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [promptEnabled, webPushEnabled]);

  const dismiss = useCallback(() => {
    try {
      window.localStorage.setItem(DISMISS_KEY, "1");
    } catch {
      // ignore — banner still hides for this session
    }
    setPhase("hidden");
  }, []);

  const turnOn = useCallback(async () => {
    setPhase("working");
    const result = await subscribeThisDevice(publicKey);
    if (result.ok) {
      toast.success("Push notifications enabled in this browser");
      setPhase("hidden");
      return;
    }
    if (result.reason === "denied") {
      // The browser will no longer prompt — hide and don't nag again.
      dismiss();
      toast.error("Notifications are blocked in your browser settings");
      return;
    }
    toast.error(result.message);
    setPhase("visible");
  }, [publicKey, dismiss]);

  if (phase === "hidden") return null;

  return (
    <div className="flex items-center gap-3 border-b bg-muted/40 px-4 py-2 text-sm">
      <Bell className="h-4 w-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1 text-foreground">
        Get notified in this browser when an issue is assigned, you&rsquo;re
        @-mentioned, or work you follow changes.
      </span>
      <a
        href={settingsHref}
        className="shrink-0 text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground"
      >
        Settings
      </a>
      <Button size="sm" onClick={() => void turnOn()} disabled={phase === "working"}>
        {phase === "working" ? (
          <>
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            Working…
          </>
        ) : (
          "Turn on"
        )}
      </Button>
      <button
        type="button"
        aria-label="Dismiss"
        onClick={dismiss}
        className="shrink-0 rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  );
}
