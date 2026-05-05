"use client";

import { useCallback, useEffect, useState } from "react";
import { Smartphone, Loader2 } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { api } from "@multica/core/api";

// Push state machine — covers everything from "browser doesn't support it" to
// "user has actively denied permission" so the UI explains what's happening.
type State =
  | { kind: "loading" }
  | { kind: "unsupported"; reason: string }
  | { kind: "server-disabled" }
  | { kind: "denied" }
  | { kind: "off"; publicKey: string }
  | { kind: "on"; publicKey: string; endpoint: string }
  | { kind: "busy"; publicKey: string };

const VAPID_KEY_LENGTH_HINT = 88;

function detectUnsupportedReason(): string | null {
  if (typeof window === "undefined") return null;
  if (!("serviceWorker" in navigator)) {
    return "This browser doesn't support service workers.";
  }
  if (!("PushManager" in window)) {
    return "This browser doesn't support Web Push.";
  }
  if (!("Notification" in window)) {
    return "This browser doesn't support notifications.";
  }
  return null;
}

// iOS only enables Web Push when the site has been added to the home screen
// AND iOS is 16.4+. We can't sniff iOS version reliably, but we can detect
// that the page is *not* running in standalone mode and warn early.
function isIOSNonStandalone(): boolean {
  if (typeof window === "undefined") return false;
  const ua = window.navigator.userAgent;
  const isIOS = /iPad|iPhone|iPod/.test(ua) ||
    (ua.includes("Macintosh") && "ontouchend" in document);
  if (!isIOS) return false;
  // standalone === true on iOS when launched from home screen.
  // matchMedia covers desktop installed PWAs and Android.
  type IOSNavigator = Navigator & { standalone?: boolean };
  const standalone = (window.navigator as IOSNavigator).standalone === true ||
    window.matchMedia("(display-mode: standalone)").matches;
  return !standalone;
}

// PushManager.subscribe wants applicationServerKey as a BufferSource (which
// requires the backing buffer to be ArrayBuffer, not ArrayBufferLike). Build
// the ArrayBuffer first and return it directly so TS narrows the type cleanly.
function urlBase64ToBuffer(base64: string): ArrayBuffer {
  const padding = "=".repeat((4 - (base64.length % 4)) % 4);
  const normalized = (base64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = window.atob(normalized);
  const buffer = new ArrayBuffer(raw.length);
  const view = new Uint8Array(buffer);
  for (let i = 0; i < raw.length; i++) view[i] = raw.charCodeAt(i);
  return buffer;
}

export function PushNotificationsSection() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const iosHint = isIOSNonStandalone();

  const refresh = useCallback(async () => {
    const reason = detectUnsupportedReason();
    if (reason) {
      setState({ kind: "unsupported", reason });
      return;
    }

    try {
      const cfg = await api.getPushPublicKey();
      if (!cfg.enabled || !cfg.publicKey) {
        setState({ kind: "server-disabled" });
        return;
      }
      if (cfg.publicKey.length < VAPID_KEY_LENGTH_HINT - 4) {
        // Defensive: refuse to subscribe with a malformed key.
        setState({ kind: "server-disabled" });
        return;
      }

      if (Notification.permission === "denied") {
        setState({ kind: "denied" });
        return;
      }

      const reg = await navigator.serviceWorker.ready;
      const existing = await reg.pushManager.getSubscription();
      if (existing) {
        setState({ kind: "on", publicKey: cfg.publicKey, endpoint: existing.endpoint });
      } else {
        setState({ kind: "off", publicKey: cfg.publicKey });
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to check push status");
      setState({ kind: "server-disabled" });
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const enable = async () => {
    if (state.kind !== "off") return;
    setState({ kind: "busy", publicKey: state.publicKey });

    try {
      const permission = await Notification.requestPermission();
      if (permission !== "granted") {
        toast.error("Notifications were not allowed");
        if (permission === "denied") setState({ kind: "denied" });
        else setState({ kind: "off", publicKey: state.publicKey });
        return;
      }

      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToBuffer(state.publicKey),
      });

      const json = sub.toJSON() as {
        endpoint?: string;
        keys?: { p256dh?: string; auth?: string };
      };
      if (!json.endpoint || !json.keys?.p256dh || !json.keys.auth) {
        throw new Error("Browser returned an incomplete subscription");
      }

      await api.subscribePush({
        endpoint: json.endpoint,
        keys: { p256dh: json.keys.p256dh, auth: json.keys.auth },
        userAgent: navigator.userAgent,
      });

      toast.success("Push notifications enabled on this device");
      setState({ kind: "on", publicKey: state.publicKey, endpoint: json.endpoint });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to enable push");
      setState({ kind: "off", publicKey: state.publicKey });
    }
  };

  const disable = async () => {
    if (state.kind !== "on") return;
    const publicKey = state.publicKey;
    setState({ kind: "busy", publicKey });

    try {
      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.getSubscription();
      const endpoint = sub?.endpoint ?? state.endpoint;
      if (sub) await sub.unsubscribe();
      await api.unsubscribePush(endpoint);
      toast.success("Push notifications turned off on this device");
      setState({ kind: "off", publicKey });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to turn off push");
      setState({ kind: "on", publicKey, endpoint: state.endpoint });
    }
  };

  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2">
        <Smartphone className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">Push notifications</h3>
        <span className="text-xs text-muted-foreground">— On this device</span>
      </div>

      <Card>
        <CardContent className="px-4 py-3">
          <PushBody
            state={state}
            iosHint={iosHint}
            onEnable={enable}
            onDisable={disable}
          />
        </CardContent>
      </Card>
    </section>
  );
}

interface PushBodyProps {
  state: State;
  iosHint: boolean;
  onEnable: () => void;
  onDisable: () => void;
}

function PushBody({ state, iosHint, onEnable, onDisable }: PushBodyProps) {
  if (state.kind === "loading") {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        Checking…
      </div>
    );
  }

  if (state.kind === "unsupported") {
    return (
      <div className="text-sm text-muted-foreground">
        Push notifications aren't available here. {state.reason}
      </div>
    );
  }

  if (state.kind === "server-disabled") {
    return (
      <div className="text-sm text-muted-foreground">
        Push notifications are not configured on the server. Ask an admin to
        set <code>VAPID_PUBLIC_KEY</code> and <code>VAPID_PRIVATE_KEY</code>.
      </div>
    );
  }

  if (state.kind === "denied") {
    return (
      <div className="text-sm text-muted-foreground">
        You've blocked notifications for this site in your browser settings.
        Re-enable them there to use push.
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="text-sm">
        {state.kind === "on" ? (
          <p className="text-foreground">Push is on for this device.</p>
        ) : (
          <p className="text-muted-foreground">
            Get push notifications when an issue is assigned, you're @-mentioned,
            or work you follow changes.
          </p>
        )}
        {iosHint && state.kind !== "on" && (
          <p className="mt-1 text-xs text-muted-foreground">
            On iPhone or iPad, first add Multica to the home screen and open
            it from there. iOS 16.4 or newer is required.
          </p>
        )}
      </div>

      {state.kind === "on" ? (
        <Button variant="outline" size="sm" onClick={onDisable}>
          Turn off
        </Button>
      ) : (
        <Button
          size="sm"
          onClick={onEnable}
          disabled={state.kind === "busy"}
        >
          {state.kind === "busy" ? (
            <>
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Working…
            </>
          ) : (
            "Turn on"
          )}
        </Button>
      )}
    </div>
  );
}
