import { useCallback, useEffect, useRef, useState } from "react";
import type { Lang } from "./copy";
import { appendDictation } from "./env";
import type { VoiceSession } from "./voice-session";

export type CommitMode = "insert" | "send";

/**
 * Lands a finished session's text: into the draft for review, or straight out
 * as a message. `landed` bumps on every insert so the editor can flash.
 */
export function useDictationCommit(
  session: VoiceSession,
  {
    lang,
    draft,
    setDraft,
    send,
    enabled = true,
  }: {
    lang: Lang;
    draft: string;
    setDraft: (value: string) => void;
    send: (text: string, viaVoice: boolean) => void;
    enabled?: boolean;
  },
) {
  const modeRef = useRef<CommitMode>("insert");
  const draftRef = useRef(draft);
  draftRef.current = draft;
  const [landed, setLanded] = useState(0);
  const [lastInsert, setLastInsert] = useState<{ before: string; raw: string } | null>(null);

  useEffect(() => {
    if (!enabled || session.phase !== "done") return;
    const before = draftRef.current;
    const text = appendDictation(before, session.result, lang);
    if (modeRef.current === "send") {
      send(text, true);
      setDraft("");
      setLastInsert(null);
    } else {
      setDraft(text);
      setLanded((n) => n + 1);
      setLastInsert({ before, raw: session.result });
    }
    modeRef.current = "insert";
    session.reset();
    // Only the transition into "done" should land text.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.phase, enabled]);

  const stopAnd = useCallback(
    (mode: CommitMode) => {
      modeRef.current = mode;
      session.stop();
    },
    [session],
  );

  return { stopAnd, landed, lastInsert, clearLastInsert: () => setLastInsert(null) };
}

/** Window-level key handling for a demo, active only while `enabled`. */
export function useWindowKeys(
  handlers: { down?: (e: KeyboardEvent) => void; up?: (e: KeyboardEvent) => void },
  enabled = true,
) {
  const ref = useRef(handlers);
  ref.current = handlers;
  useEffect(() => {
    if (!enabled) return;
    const down = (e: KeyboardEvent) => ref.current.down?.(e);
    const up = (e: KeyboardEvent) => ref.current.up?.(e);
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
    };
  }, [enabled]);
}

/** ⌘⇧Space on macOS, Ctrl+Shift+Space elsewhere. */
export function isToggleChord(e: KeyboardEvent): boolean {
  return e.code === "Space" && e.shiftKey && (e.metaKey || e.ctrlKey) && !e.repeat;
}

export function isActive(phase: VoiceSession["phase"]): boolean {
  return phase === "listening" || phase === "paused" || phase === "requesting";
}
