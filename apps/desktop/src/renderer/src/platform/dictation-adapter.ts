import type { DictationAdapter } from "@multica/core/types/dictation";

// Keep Windows on the native path even during a mismatched preload/HMR boot.
// An unavailable native bridge must never turn into a paid API fallback.
export const desktopDictationAdapter: DictationAdapter = {
  async toggle() {
    const bridge = window.desktopAPI?.dictation;
    if (!bridge) return { ok: false, reason: "unavailable" };
    return bridge.toggle();
  },
};
