// React Native defines `window` as its global object, without DOM events, so
// `typeof window` alone does not tell whether listeners can be attached.
function eventWindow(): Window | null {
  if (typeof window === "undefined") return null;
  return typeof window.addEventListener === "function" ? window : null;
}

export function addWindowListener<K extends keyof WindowEventMap>(
  type: K,
  listener: (event: WindowEventMap[K]) => void,
  options?: AddEventListenerOptions,
): void {
  eventWindow()?.addEventListener(type, listener, options);
}

export function removeWindowListener<K extends keyof WindowEventMap>(
  type: K,
  listener: (event: WindowEventMap[K]) => void,
): void {
  eventWindow()?.removeEventListener(type, listener);
}
