import { useSyncExternalStore } from "react";

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
 * Conservative by design: any uncertainty (SSR, missing DOM) resolves to
 * `true`, and the moment focus/visibility is lost it flips to `false`, so we err
 * toward showing the badge — which clears itself as soon as the user returns.
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
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
