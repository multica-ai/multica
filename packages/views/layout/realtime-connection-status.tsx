"use client";

import { useEffect, useState } from "react";
import { useWS } from "@multica/core/realtime";
import { Button } from "@multica/ui/components/ui/button";
import { Loader2, WifiOff } from "@multica/ui/icons";
import { useT } from "../i18n";

const SHOW_DELAY_MS = 2_000;

export function RealtimeConnectionStatus() {
  const { connectionState, reconnect } = useWS();
  const { t } = useT("layout");
  const [visible, setVisible] = useState(false);
  const unavailable =
    connectionState === "disconnected" || connectionState === "reconnecting";

  useEffect(() => {
    if (!unavailable) {
      setVisible(false);
      return;
    }
    if (visible) return;

    const timer = window.setTimeout(() => setVisible(true), SHOW_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [unavailable, visible]);

  if (!visible || !unavailable) return null;

  const reconnecting = connectionState === "reconnecting";

  return (
    <div
      className="flex min-h-9 shrink-0 items-center justify-center gap-2 border-b border-warning/30 bg-warning/10 px-3 text-caption text-foreground"
      role="status"
      aria-live="polite"
    >
      {reconnecting ? (
        <Loader2 className="size-3.5 animate-spin" aria-hidden />
      ) : (
        <WifiOff className="size-3.5" aria-hidden />
      )}
      <span>
        {reconnecting
          ? t(($) => $.realtime.reconnecting)
          : t(($) => $.realtime.disconnected)}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        disabled={reconnecting}
        onClick={reconnect}
      >
        {t(($) => $.realtime.reconnect)}
      </Button>
    </div>
  );
}
