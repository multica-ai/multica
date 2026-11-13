import { createContext, useContext, type ReactNode } from "react";
import { COPY, type Copy, type Lang } from "./copy";
import type { VoiceSource } from "./voice-session";

interface StoryEnv {
  lang: Lang;
  source: VoiceSource;
}

const StoryEnvContext = createContext<StoryEnv>({ lang: "en", source: "simulated" });

export function StoryEnvProvider({ lang, source, children }: StoryEnv & { children: ReactNode }) {
  return <StoryEnvContext.Provider value={{ lang, source }}>{children}</StoryEnvContext.Provider>;
}

export function useStoryEnv(): StoryEnv & { copy: Copy } {
  const env = useContext(StoryEnvContext);
  return { ...env, copy: COPY[env.lang] };
}

/** Appends dictated text to a draft the way a person would have typed it. */
export function appendDictation(draft: string, text: string, lang: Lang): string {
  if (!text) return draft;
  if (!draft.trim()) return text;
  const sep = lang === "zh" || /\s$/.test(draft) ? "" : " ";
  return draft + sep + text;
}
