import { SOUND_DEFINITIONS, THEME_VOLUME_MAP, type SoundType, type SoundTheme } from "./sound-definitions";

/**
 * Browser-based sound playback using Web Audio API sine-wave synthesis.
 *
 * No external audio files — avoids <audio> autoplay restrictions and works
 * across modern browsers. AudioContext is lazily initialised on first user
 * interaction (click / keydown) to comply with browser autoplay policies.
 */
export class SoundManager {
  private ctx: AudioContext | null = null;
  private initialized = false;
  private resumed = false;

  /**
   * Lazy-init the AudioContext. Safe to call repeatedly — subsequent calls
   * are no-ops once the context exists.
   */
  init(): void {
    if (this.initialized) return;
    try {
      this.ctx = new AudioContext();
    } catch {
      // Web Audio API unavailable (SSR, ancient browser, or cross-origin
      // iframe without allow="autoplay"). Remaining methods are no-ops.
      return;
    }
    this.initialized = true;
    this.registerResumeListeners();
  }

  /**
   * Register document-level event listeners that resume a suspended
   * AudioContext on first user interaction. Required by browser policy:
   * AudioContexts created before user gesture start in "suspended" state.
   */
  private registerResumeListeners(): void {
    if (this.resumed) return;
    this.resumed = true;

    const resume = () => {
      this.ctx?.resume();
      if (this.ctx?.state === "running") {
        document.removeEventListener("click", resume);
        document.removeEventListener("keydown", resume);
      }
    };

    document.addEventListener("click", resume);
    document.addEventListener("keydown", resume);
  }

  /**
   * Play a sine-wave tone sequence for the given sound type.
   *
   * Silently skips when:
   * - AudioContext is unavailable (SSR / unsupported browser)
   * - AudioContext is suspended (user hasn't interacted yet)
   * - volume is zero
   *
   * @param type  - sound category determining the tone sequence
   * @param volume - 0–100 (user preference slider value)
   * @param theme  - volume multiplier theme ("default" | "soft" | "alert")
   */
  play(type: SoundType, volume: number = 70, theme: SoundTheme = "default"): void {
    if (!this.ctx || this.ctx.state !== "running") return;
    if (volume <= 0) return;

    const tones = SOUND_DEFINITIONS[type];
    if (!tones || tones.length === 0) return;

    const themeMultiplier = THEME_VOLUME_MAP[theme] ?? 1.0;
    const gain = (volume / 100) * themeMultiplier;

    const now = this.ctx.currentTime;

    for (const [freq, start, duration] of tones) {
      const osc = this.ctx.createOscillator();
      const gainNode = this.ctx.createGain();

      osc.type = "sine";
      osc.frequency.value = freq;

      gainNode.gain.setValueAtTime(gain, now + start);
      gainNode.gain.exponentialRampToValueAtTime(0.001, now + start + duration);

      osc.connect(gainNode).connect(this.ctx.destination);
      osc.start(now + start);
      osc.stop(now + start + duration);
    }
  }
}

/** Singleton instance — one AudioContext per page is the recommended pattern. */
export const soundManager = new SoundManager();
