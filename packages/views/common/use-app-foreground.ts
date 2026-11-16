import { useEffect, useState, useSyncExternalStore } from "react";

interface NativeForegroundAPI {
  getWindowForeground: () => Promise<boolean>;
  onWindowForegroundChanged: (callback: (foreground: boolean) => void) => () => void;
}

function nativeForegroundAPI(): NativeForegroundAPI | undefined {
  if (typeof window === "undefined") return undefined;
  const api = (window as unknown as { desktopAPI?: Partial<NativeForegroundAPI> }).desktopAPI;
  if (typeof api?.getWindowForeground === "function" && typeof api.onWindowForegroundChanged === "function") {
    return api as NativeForegroundAPI;
  }
  return undefined;
}

/**
 * Whether the app window is actually in front of the user right now: the
 * document is visible (not a background browser tab / minimized window) AND the
 * window holds OS focus (not sitting behind another app or another window).
 *
 * Callers use it to answer "is the user really looking at this surface". A chat
 * reply that lands while the app is NOT in the foreground must not be
 * auto-marked-read and must still raise the unread badge — otherwise the
 * notification is silently eaten while the user is away (MUL-4485). The chat
 * sidebar badge and the auto mark-read effects share this signal so they stay
 * consistent: while backgrounded the active session both counts toward the
 * badge and keeps its unread; on return the badge clears and mark-read fires.
 *
 * Desktop uses the native window signal because Electron's DOM focus can
 * remain true after the application loses OS focus. Until the initial native
 * query resolves, keep replies unread. In a browser the DOM is authoritative.
 * The `true` fallbacks below only apply where there is no window — SSR and
 * other non-DOM contexts — which also have no live surface or WS traffic to act
 * on. `true` (foreground) is chosen there only so the SSR snapshot matches the
 * first client snapshot of a freshly opened, focused window and hydration
 * doesn't flip the value; the real focus/visibility state takes over
 * immediately on the client, flipping to `false` the moment either is lost.
 */
function subscribe(onStoreChange: () => void): () => void {
  if (typeof document === "undefined") return () => {};
  document.addEventListener("visibilitychange", onStoreChange);
  window.addEventListener("focus", onStoreChange);
  window.addEventListener("blur", onStoreChange);
  return () => {
    document.removeEventListener("visibilitychange", onStoreChange);
    window.removeEventListener("focus", onStoreChange);
    window.removeEventListener("blur", onStoreChange);
  };
}

function getSnapshot(): boolean {
  if (typeof document === "undefined") return true;
  return document.visibilityState === "visible" && document.hasFocus();
}

// Server render has no window to be in the foreground of; default to visible so
// the first client paint matches SSR, then useSyncExternalStore reconciles to
// the real value on hydration.
function getServerSnapshot(): boolean {
  return true;
}

export function useAppForeground(): boolean {
  const domForeground = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
  const api = nativeForegroundAPI();
  const [nativeForeground, setNativeForeground] = useState(false);
  useEffect(() => {
    if (!api) return;
    let live = true;
    let receivedEvent = false;
    const unsubscribe = api.onWindowForegroundChanged((foreground) => {
      receivedEvent = true;
      setNativeForeground(foreground);
    });
    void api.getWindowForeground().then((foreground) => {
      if (live && !receivedEvent) setNativeForeground(foreground);
    }).catch(() => { /* Keep unread until native focus can be confirmed. */ });
    return () => { live = false; unsubscribe(); };
  }, [api]);
  return api ? nativeForeground : domForeground;
}
