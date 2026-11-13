import { useCallback, useEffect, useRef, useState } from "react";
import type { Lang, VoiceScript } from "./copy";

export type VoicePhase = "idle" | "requesting" | "listening" | "paused" | "processing" | "done" | "error";
export type VoiceError = "permission-denied" | "no-speech" | "network";
export type VoiceSource = "simulated" | "microphone";

export interface VoiceSnapshot {
  phase: VoicePhase;
  /** Smoothed input level, 0..1. */
  level: number;
  /** Level history, oldest first, one sample every SAMPLE_MS. */
  levels: number[];
  elapsedMs: number;
  finalText: string;
  interimText: string;
  /** What gets inserted once the session is done. */
  result: string;
  error: VoiceError | null;
}

export interface VoiceSession extends VoiceSnapshot {
  start: () => void;
  stop: () => void;
  cancel: () => void;
  pause: () => void;
  resume: () => void;
  reset: () => void;
  /** Re-transcribe the kept recording after a transcription failure; otherwise start over. */
  retry: () => void;
}

export interface VoiceSessionOptions {
  source: VoiceSource;
  lang: Lang;
  script: VoiceScript;
  /** Return the polished text instead of the raw transcript. */
  polish?: boolean;
  processingMs?: number;
  /** Simulated failure, to walk through error states. */
  failWith?: VoiceError | null;
  /** Simulated permission: "prompt" shows the request step on the first start. */
  permission?: "granted" | "prompt";
}

export const SAMPLE_MS = 50;
export const HISTORY = 160;

const EMPTY: VoiceSnapshot = {
  phase: "idle",
  level: 0,
  levels: Array.from({ length: HISTORY }, () => 0),
  elapsedMs: 0,
  finalText: "",
  interimText: "",
  result: "",
  error: null,
};

const FILLERS = /^(um,?\s*|uh,?\s*|嗯，?|呃，?)/i;

function tidy(text: string): string {
  const t = text.trim().replace(FILLERS, "");
  return t.charAt(0).toUpperCase() + t.slice(1);
}

/** Where the simulated speaker is at `t` ms into the recording. */
function scriptAt(script: VoiceScript, lang: Lang, t: number) {
  const msPerChar = lang === "zh" ? 210 : 52;
  const gap = 420;
  let cursor = 650;
  let finalText = "";
  for (const phrase of script.phrases) {
    const duration = phrase.length * msPerChar;
    if (t < cursor) return { finalText, interimText: "", speaking: false, finished: false };
    if (t < cursor + duration) {
      const shown = Math.floor((t - cursor) / msPerChar);
      // Recognizers emit whole words (or two-character chunks in Chinese), not letters.
      let cut = lang === "zh" ? shown - (shown % 2) : phrase.lastIndexOf(" ", shown);
      while (cut > 0 && /[A-Za-z]/.test(phrase[cut - 1]!) && /[A-Za-z]/.test(phrase[cut] ?? "")) cut--;
      return { finalText, interimText: phrase.slice(0, Math.max(0, cut)), speaking: true, finished: false };
    }
    finalText += phrase;
    cursor += duration + gap;
  }
  return { finalText, interimText: "", speaking: false, finished: true };
}

/** A believable speech envelope: syllable-rate wobble on top of noise. */
function speechLevel(t: number, speaking: boolean): number {
  if (!speaking) return 0.03 + Math.random() * 0.04;
  const syllable = 0.5 + 0.5 * Math.sin(t / 70) * Math.sin(t / 190 + 1.3);
  return Math.min(1, 0.22 + syllable * 0.55 + Math.random() * 0.2);
}

type SpeechRecognitionLike = {
  lang: string;
  continuous: boolean;
  interimResults: boolean;
  start: () => void;
  stop: () => void;
  abort: () => void;
  onresult: ((event: { resultIndex: number; results: ArrayLike<ArrayLike<{ transcript: string }> & { isFinal: boolean }> }) => void) | null;
  onerror: ((event: { error: string }) => void) | null;
  onend: (() => void) | null;
};

function createRecognizer(): SpeechRecognitionLike | null {
  const w = window as unknown as Record<string, (new () => SpeechRecognitionLike) | undefined>;
  const Ctor = w.SpeechRecognition ?? w.webkitSpeechRecognition;
  return Ctor ? new Ctor() : null;
}

interface MicRig {
  stream: MediaStream;
  context: AudioContext;
  analyser: AnalyserNode;
  buffer: Float32Array<ArrayBuffer>;
  recognizer: SpeechRecognitionLike | null;
  recognized: { final: string; interim: string };
  wantRecognition: boolean;
}

export function useVoiceSession(options: VoiceSessionOptions): VoiceSession {
  const [snap, setSnap] = useState<VoiceSnapshot>(EMPTY);
  const optsRef = useRef(options);
  optsRef.current = options;
  const stateRef = useRef<VoiceSnapshot>(EMPTY);
  const timerRef = useRef<number | null>(null);
  const phaseStartedRef = useRef(0);
  const permissionGrantedRef = useRef(false);
  const micRef = useRef<MicRig | null>(null);
  const lastTickRef = useRef(0);
  const retriedRef = useRef(false);

  const commit = useCallback((patch: Partial<VoiceSnapshot>) => {
    stateRef.current = { ...stateRef.current, ...patch };
    setSnap(stateRef.current);
  }, []);

  const teardownMic = useCallback(() => {
    const rig = micRef.current;
    micRef.current = null;
    if (!rig) return;
    rig.wantRecognition = false;
    rig.recognizer?.abort();
    rig.stream.getTracks().forEach((track) => track.stop());
    void rig.context.close();
  }, []);

  const stopTimer = useCallback(() => {
    if (timerRef.current !== null) window.clearInterval(timerRef.current);
    timerRef.current = null;
  }, []);

  const finish = useCallback(() => {
    const s = stateRef.current;
    const { script, polish, failWith } = optsRef.current;
    const spoken = (s.finalText + s.interimText).trim();
    if (!spoken) {
      commit({ phase: "error", error: "no-speech", level: 0 });
      stopTimer();
      return;
    }
    if (failWith === "network" && !retriedRef.current) {
      commit({ phase: "error", error: "network", level: 0 });
      stopTimer();
      return;
    }
    const completeScript = spoken === script.phrases.join("").trim();
    const result = polish ? (completeScript && optsRef.current.source === "simulated" ? script.polished : tidy(spoken)) : spoken;
    commit({ phase: "done", finalText: spoken, interimText: "", result, level: 0 });
    stopTimer();
  }, [commit, stopTimer]);

  const tick = useCallback(() => {
    const now = performance.now();
    const dt = now - lastTickRef.current;
    lastTickRef.current = now;
    const s = stateRef.current;
    const opts = optsRef.current;

    if (s.phase === "requesting") {
      if (opts.source === "simulated" && now - phaseStartedRef.current > 1400) {
        if (opts.failWith === "permission-denied") {
          commit({ phase: "error", error: "permission-denied" });
          stopTimer();
          return;
        }
        permissionGrantedRef.current = true;
        phaseStartedRef.current = now;
        commit({ phase: "listening", elapsedMs: 0 });
      }
      return;
    }

    if (s.phase === "processing") {
      if (now - phaseStartedRef.current > (opts.processingMs ?? 900)) finish();
      return;
    }

    if (s.phase === "paused") {
      commit({ level: 0, levels: [...s.levels.slice(1), 0] });
      return;
    }

    if (s.phase !== "listening") return;

    const elapsedMs = s.elapsedMs + dt;
    let level: number;
    let finalText = s.finalText;
    let interimText = s.interimText;

    const rig = micRef.current;
    if (opts.source === "microphone" && rig) {
      rig.analyser.getFloatTimeDomainData(rig.buffer);
      let sum = 0;
      for (const v of rig.buffer) sum += v * v;
      const rms = Math.sqrt(sum / rig.buffer.length);
      level = Math.min(1, Math.pow(rms * 6, 0.75));
      if (rig.recognizer) {
        finalText = rig.recognized.final;
        interimText = rig.recognized.interim;
      } else {
        const at = scriptAt(opts.script, opts.lang, elapsedMs);
        finalText = at.finalText;
        interimText = at.interimText;
      }
    } else {
      const silent = opts.failWith === "no-speech";
      const at = silent
        ? { finalText: "", interimText: "", speaking: false }
        : scriptAt(opts.script, opts.lang, elapsedMs);
      level = speechLevel(elapsedMs, at.speaking);
      finalText = at.finalText;
      interimText = at.interimText;
      if (silent && elapsedMs > 3600) {
        commit({ phase: "error", error: "no-speech", level: 0 });
        stopTimer();
        return;
      }
    }

    const smoothed = s.level * 0.45 + level * 0.55;
    commit({
      elapsedMs,
      level: smoothed,
      levels: [...s.levels.slice(1), smoothed],
      finalText,
      interimText,
    });
  }, [commit, finish, stopTimer]);

  const ensureTimer = useCallback(() => {
    if (timerRef.current !== null) return;
    lastTickRef.current = performance.now();
    timerRef.current = window.setInterval(tick, SAMPLE_MS);
  }, [tick]);

  const startMic = useCallback(async () => {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      if (stateRef.current.phase !== "requesting") {
        stream.getTracks().forEach((track) => track.stop());
        return;
      }
      const context = new AudioContext();
      const analyser = context.createAnalyser();
      analyser.fftSize = 1024;
      context.createMediaStreamSource(stream).connect(analyser);
      const recognizer = createRecognizer();
      const rig: MicRig = {
        stream,
        context,
        analyser,
        buffer: new Float32Array(analyser.fftSize),
        recognizer,
        recognized: { final: "", interim: "" },
        wantRecognition: true,
      };
      if (recognizer) {
        recognizer.lang = optsRef.current.lang === "zh" ? "zh-CN" : "en-US";
        recognizer.continuous = true;
        recognizer.interimResults = true;
        let committed = "";
        recognizer.onresult = (event) => {
          let interim = "";
          for (let i = event.resultIndex; i < event.results.length; i++) {
            const result = event.results[i]!;
            const text = result[0]?.transcript ?? "";
            if (result.isFinal) committed += text;
            else interim += text;
          }
          rig.recognized = { final: committed, interim };
        };
        recognizer.onerror = (event) => {
          if (event.error === "not-allowed") commit({ phase: "error", error: "permission-denied" });
          if (event.error === "network") commit({ phase: "error", error: "network" });
        };
        // Chrome ends continuous recognition on its own after a pause; keep it going.
        recognizer.onend = () => {
          if (rig.wantRecognition && stateRef.current.phase === "listening") recognizer.start();
        };
        recognizer.start();
      }
      micRef.current = rig;
      permissionGrantedRef.current = true;
      phaseStartedRef.current = performance.now();
      commit({ phase: "listening", elapsedMs: 0 });
    } catch {
      commit({ phase: "error", error: "permission-denied" });
      stopTimer();
    }
  }, [commit, stopTimer]);

  const start = useCallback(() => {
    const phase = stateRef.current.phase;
    if (phase === "listening" || phase === "requesting" || phase === "processing") return;
    teardownMic();
    retriedRef.current = false;
    stateRef.current = { ...EMPTY };
    const opts = optsRef.current;
    phaseStartedRef.current = performance.now();
    const needsPrompt =
      opts.source === "microphone" ||
      opts.failWith === "permission-denied" ||
      (opts.permission === "prompt" && !permissionGrantedRef.current);
    commit({ phase: needsPrompt ? "requesting" : "listening" });
    ensureTimer();
    if (opts.source === "microphone") void startMic();
  }, [commit, ensureTimer, startMic, teardownMic]);

  const stop = useCallback(() => {
    const phase = stateRef.current.phase;
    if (phase === "requesting") {
      teardownMic();
      stopTimer();
      commit({ ...EMPTY });
      return;
    }
    if (phase !== "listening" && phase !== "paused") return;
    const rig = micRef.current;
    if (rig) {
      rig.wantRecognition = false;
      rig.recognizer?.stop();
      // Give the recognizer a moment to deliver its last final result.
      window.setTimeout(() => {
        const r = micRef.current?.recognized;
        if (r) commit({ finalText: r.final, interimText: r.interim });
        teardownMic();
      }, 500);
    }
    phaseStartedRef.current = performance.now();
    commit({ phase: "processing", level: 0 });
    ensureTimer();
  }, [commit, ensureTimer, stopTimer, teardownMic]);

  const cancel = useCallback(() => {
    teardownMic();
    stopTimer();
    commit({ ...EMPTY });
  }, [commit, stopTimer, teardownMic]);

  const pause = useCallback(() => {
    if (stateRef.current.phase !== "listening") return;
    micRef.current?.recognizer?.stop();
    if (micRef.current) micRef.current.wantRecognition = false;
    commit({ phase: "paused", level: 0 });
  }, [commit]);

  const resume = useCallback(() => {
    if (stateRef.current.phase !== "paused") return;
    const rig = micRef.current;
    if (rig?.recognizer) {
      rig.wantRecognition = true;
      rig.recognizer.start();
    }
    lastTickRef.current = performance.now();
    commit({ phase: "listening" });
  }, [commit]);

  const retry = useCallback(() => {
    if (stateRef.current.error !== "network") {
      start();
      return;
    }
    retriedRef.current = true;
    phaseStartedRef.current = performance.now();
    commit({ phase: "processing", error: null });
    ensureTimer();
  }, [commit, ensureTimer, start]);

  useEffect(() => () => {
    stopTimer();
    teardownMic();
  }, [stopTimer, teardownMic]);

  return { ...snap, start, stop, cancel, pause, resume, reset: cancel, retry };
}

/** Deterministic speech-like level history, for frozen state stories. */
function syntheticLevels(seed: number, speaking: boolean): number[] {
  return Array.from({ length: HISTORY }, (_, i) => {
    if (!speaking) return 0.04;
    const t = (i + seed) * SAMPLE_MS;
    const word = Math.sin(t / 900 + seed) > -0.55 ? 1 : 0.1;
    const syllable = 0.5 + 0.5 * Math.sin(t / 70) * Math.sin(t / 190 + 1.3);
    const noise = (Math.sin(i * 12.9898 + seed * 78.233) * 43758.5453) % 1;
    return Math.min(1, word * (0.2 + syllable * 0.55) + Math.abs(noise) * 0.12);
  });
}

const noop = () => {};

/** A frozen session in any phase, for side-by-side state stories. */
export function mockSession(patch: Partial<VoiceSnapshot> & { speaking?: boolean } = {}): VoiceSession {
  const { speaking = patch.phase === "listening", ...rest } = patch;
  const levels = rest.levels ?? syntheticLevels(7, speaking);
  return {
    ...EMPTY,
    levels,
    level: rest.level ?? levels[levels.length - 1] ?? 0,
    ...rest,
    start: noop,
    stop: noop,
    cancel: noop,
    pause: noop,
    resume: noop,
    reset: noop,
    retry: noop,
  };
}
