import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";

/** How the voice shortcut and the mic button react to a press. */
export type Activation = "auto" | "toggle" | "hold";
export type Engine = "local" | "cloud";
/** Off; a Polish chip after inserting (B's idea, borrowed into A); or always. */
export type PolishMode = "off" | "offer" | "auto";
export type Platform = "desktop" | "web";

export interface LocalModel {
  id: string;
  name: string;
  sizeMb: number;
  languages: { en: string; zh: string };
  /** 1–3, for the speed and accuracy dots. */
  speed: number;
  accuracy: number;
  /** Streams text while you talk, so the HUD can show it live. */
  streaming: boolean;
  recommendedFor?: "zh" | "en";
}

/**
 * Local speech models in the shape Handy ships them (GGUF via transcribe-cpp).
 * Sizes and language coverage are from Handy's catalog; the dots are a rough
 * guide for the mock, not benchmarks.
 */
export const LOCAL_MODELS: LocalModel[] = [
  {
    id: "sense-voice",
    name: "SenseVoice Small",
    sizeMb: 152,
    languages: { en: "Chinese, English, Japanese, Korean, Cantonese", zh: "中文、英文、日语、韩语、粤语" },
    speed: 3,
    accuracy: 2,
    streaming: false,
    recommendedFor: "zh",
  },
  {
    id: "parakeet-v3",
    name: "Parakeet V3",
    sizeMb: 456,
    languages: { en: "English + 24 European languages, auto-detect", zh: "英文 + 24 种欧洲语言，自动识别" },
    speed: 3,
    accuracy: 3,
    streaming: false,
    recommendedFor: "en",
  },
  {
    id: "whisper-turbo",
    name: "Whisper Large v3 Turbo",
    sizeMb: 1620,
    languages: { en: "99 languages", zh: "99 种语言" },
    speed: 1,
    accuracy: 3,
    streaming: false,
  },
  {
    id: "moonshine-small",
    name: "Moonshine V2 Small",
    sizeMb: 99,
    languages: { en: "English only, streams as you talk", zh: "仅英文，边说边出字" },
    speed: 3,
    accuracy: 2,
    streaming: true,
  },
];

export type ModelStatus = { state: "available" } | { state: "downloading"; progress: number } | { state: "ready" };

export interface VoiceSettings {
  platform: Platform;
  shortcut: string[];
  activation: Activation;
  /** Also answer the shortcut while Multica is in the background (desktop). */
  globalShortcut: boolean;
  engine: Engine;
  model: string;
  language: "auto" | "zh" | "en" | "ja";
  chineseScript: "simplified" | "traditional";
  microphone: string;
  sounds: boolean;
  muteOthers: boolean;
  removeFillers: boolean;
  polish: PolishMode;
  vocabulary: string[];
  keepRecordings: "off" | "7d" | "30d";
  /** Permission state of the OS / browser microphone prompt. */
  micPermission: "granted" | "prompt" | "denied";
  models: Record<string, ModelStatus>;
}

export const DEFAULT_SETTINGS: VoiceSettings = {
  platform: "desktop",
  shortcut: ["⌥", "Space"],
  activation: "auto",
  globalShortcut: false,
  engine: "local",
  model: "sense-voice",
  language: "auto",
  chineseScript: "simplified",
  microphone: "default",
  sounds: true,
  muteOthers: false,
  removeFillers: true,
  polish: "offer",
  vocabulary: ["Multica", "Lambda", "Tiptap", "workspace ID"],
  keepRecordings: "off",
  micPermission: "granted",
  models: {
    "sense-voice": { state: "ready" },
    "parakeet-v3": { state: "available" },
    "whisper-turbo": { state: "available" },
    "moonshine-small": { state: "available" },
  },
};

interface VoiceSettingsContext {
  settings: VoiceSettings;
  update: (patch: Partial<VoiceSettings>) => void;
  download: (id: string) => void;
  remove: (id: string) => void;
  /** The model in use when the engine is local, if it is on disk. */
  activeModel: LocalModel | null;
}

const Context = createContext<VoiceSettingsContext | null>(null);

export function VoiceSettingsProvider({
  initial,
  children,
}: {
  initial?: Partial<VoiceSettings>;
  children: ReactNode;
}) {
  const [settings, setSettings] = useState<VoiceSettings>(() => ({
    ...DEFAULT_SETTINGS,
    ...initial,
    models: { ...DEFAULT_SETTINGS.models, ...initial?.models },
  }));
  const timers = useRef(new Map<string, number>());
  useEffect(() => {
    const map = timers.current;
    return () => map.forEach((t) => window.clearInterval(t));
  }, []);

  const update = useCallback((patch: Partial<VoiceSettings>) => setSettings((s) => ({ ...s, ...patch })), []);

  const download = useCallback((id: string) => {
    const model = LOCAL_MODELS.find((m) => m.id === id);
    if (!model || timers.current.has(id)) return;
    // Paced so a 150 MB model takes about four seconds in the demo.
    const step = Math.max(0.6, 2400 / model.sizeMb);
    setSettings((s) => ({ ...s, models: { ...s.models, [id]: { state: "downloading", progress: 0 } } }));
    const t = window.setInterval(() => {
      setSettings((s) => {
        const cur = s.models[id];
        const progress = cur?.state === "downloading" ? Math.min(100, cur.progress + step) : 100;
        if (progress >= 100) {
          window.clearInterval(timers.current.get(id));
          timers.current.delete(id);
          return { ...s, models: { ...s.models, [id]: { state: "ready" } } };
        }
        return { ...s, models: { ...s.models, [id]: { state: "downloading", progress } } };
      });
    }, 60);
    timers.current.set(id, t);
  }, []);

  const remove = useCallback((id: string) => {
    window.clearInterval(timers.current.get(id));
    timers.current.delete(id);
    setSettings((s) => ({ ...s, models: { ...s.models, [id]: { state: "available" } } }));
  }, []);

  const value = useMemo<VoiceSettingsContext>(() => {
    const model = LOCAL_MODELS.find((m) => m.id === settings.model) ?? null;
    const ready = model && settings.models[model.id]?.state === "ready";
    return { settings, update, download, remove, activeModel: ready ? model : null };
  }, [settings, update, download, remove]);

  return <Context.Provider value={value}>{children}</Context.Provider>;
}

const FALLBACK: VoiceSettingsContext = {
  settings: DEFAULT_SETTINGS,
  update: () => {},
  download: () => {},
  remove: () => {},
  activeModel: LOCAL_MODELS[0]!,
};

export function useVoiceSettings(): VoiceSettingsContext {
  return useContext(Context) ?? FALLBACK;
}

export function formatSize(mb: number): string {
  return mb >= 1000 ? `${(mb / 1024).toFixed(1)} GB` : `${mb} MB`;
}
