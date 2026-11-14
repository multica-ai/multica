import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { CHAT_SCRIPT, type VoiceScript } from "../copy";
import { appendDictation, useStoryEnv } from "../env";
import { useVoiceSession, type VoiceSession, type VoiceSessionOptions } from "../voice-session";
import { useVoiceSettings } from "../settings/store";

/**
 * One voice layer for every composer. The provider owns the single recording
 * (there is never more than one), the shortcut, and the commit; a composer
 * only registers how to read and write its draft. Chat, issue comments and
 * thread replies all plug in the same way, so they behave the same.
 *
 * Presentation follows the press, not the composer:
 *   tap  → "strip": the composer's toolbar becomes the recording strip (A)
 *   hold → "hud":   a floating pill above the composer; release inserts (C)
 */

export type VoiceMode = "pending" | "strip" | "hud";
export type CommitKind = "insert" | "send";

export interface VoiceTargetSpec {
  id: string;
  /** What the simulated speaker says into this composer. */
  script: VoiceScript;
  getDraft: () => string;
  setDraft: (value: string) => void;
  send: (text: string) => void;
}

interface PolishOffer {
  id: string;
  before: string;
  raw: string;
}

interface VoiceContextValue {
  session: VoiceSession;
  activeId: string | null;
  mode: VoiceMode;
  focusedId: string | null;
  landed: Record<string, number>;
  offer: PolishOffer | null;
  polishing: boolean;
  register: (spec: VoiceTargetSpec) => () => void;
  focus: (id: string) => void;
  press: (id: string, via: "key" | "pointer") => void;
  release: () => void;
  stopAnd: (kind: CommitKind) => void;
  cancel: () => void;
  retry: () => void;
  polish: () => void;
  dismissOffer: () => void;
}

/** Shorter than this is a tap (toggle), longer is a hold (push-to-talk). */
export const TAP_MS = 250;

const FILLERS = /^(um,?\s*|uh,?\s*|嗯，?|呃，?)/i;

const VoiceContext = createContext<VoiceContextValue | null>(null);

export interface FrozenVoice {
  session: VoiceSession;
  activeId: string | null;
  mode?: VoiceMode;
  offerFor?: string;
}

export function VoiceProvider({
  primaryId,
  frozen,
  simulate,
  gate,
  children,
}: {
  /** Where the shortcut sends text when no composer has had focus yet. */
  primaryId: string;
  /** Pin the layer at one state, for galleries. */
  frozen?: FrozenVoice;
  simulate?: Pick<VoiceSessionOptions, "failWith" | "permission">;
  /** Runs before a recording starts; return false to hold it (e.g. first-use setup). */
  gate?: () => boolean;
  children: ReactNode;
}) {
  const { lang, source } = useStoryEnv();
  const { settings } = useVoiceSettings();
  const targets = useRef(new Map<string, VoiceTargetSpec>());
  const [activeId, setActiveId] = useState<string | null>(null);
  const [mode, setMode] = useState<VoiceMode>("strip");
  const [focusedId, setFocusedId] = useState<string | null>(null);
  const [landed, setLanded] = useState<Record<string, number>>({});
  const [offer, setOffer] = useState<PolishOffer | null>(null);
  const [polishing, setPolishing] = useState(false);
  const pressRef = useRef<{ at: number; via: "key" | "pointer" } | null>(null);
  const commitRef = useRef<CommitKind>("insert");

  const script = (activeId && targets.current.get(activeId)?.script) || CHAT_SCRIPT[lang];
  const live = useVoiceSession({
    source,
    lang,
    script,
    polish: settings.polish === "auto",
    processingMs: settings.engine === "local" ? 650 : 1000,
    permission: settings.micPermission === "prompt" ? "prompt" : "granted",
    failWith: settings.micPermission === "denied" ? "permission-denied" : null,
    ...simulate,
  });
  const session = frozen?.session ?? live;
  const { phase } = session;

  // Land the text once the recording is transcribed.
  const activeRef = useRef(activeId);
  activeRef.current = activeId;
  useEffect(() => {
    if (frozen || live.phase !== "done") return;
    const target = activeRef.current ? targets.current.get(activeRef.current) : undefined;
    if (target) {
      const raw = settings.removeFillers ? live.result.replace(FILLERS, "") : live.result;
      const before = target.getDraft();
      const text = appendDictation(before, raw, lang);
      if (commitRef.current === "send") {
        target.send(text);
        target.setDraft("");
        setOffer(null);
      } else {
        target.setDraft(text);
        setLanded((m) => ({ ...m, [target.id]: (m[target.id] ?? 0) + 1 }));
        setOffer(settings.polish === "offer" ? { id: target.id, before, raw: live.result } : null);
      }
    }
    commitRef.current = "insert";
    live.reset();
    // Only the transition into "done" should land text.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [live.phase]);

  // Back to idle (landed or cancelled): release the composer.
  useEffect(() => {
    if (!frozen && live.phase === "idle") {
      setActiveId(null);
      pressRef.current = null;
    }
  }, [frozen, live.phase]);

  // A press that is still down after TAP_MS is a hold.
  useEffect(() => {
    if (mode !== "pending") return;
    const t = window.setTimeout(() => {
      if (pressRef.current) setMode("hud");
    }, TAP_MS);
    return () => window.clearTimeout(t);
  }, [mode]);

  // The offer only stands while the inserted text is the newest thing.
  useEffect(() => {
    if (!offer) return;
    const t = window.setTimeout(() => setOffer(null), 9000);
    return () => window.clearTimeout(t);
  }, [offer]);

  const stopAnd = useCallback(
    (kind: CommitKind) => {
      commitRef.current = kind;
      pressRef.current = null;
      live.stop();
    },
    [live],
  );

  const press = useCallback(
    (id: string, via: "key" | "pointer") => {
      if (frozen) return;
      const s = live.phase;
      if (s === "listening" || s === "requesting" || s === "paused") {
        // A second press ends a tapped (strip) recording.
        if (mode === "strip") stopAnd("insert");
        return;
      }
      if (s !== "idle" && s !== "error") return;
      if (gate && !gate()) return;
      setOffer(null);
      setActiveId(id);
      const next: VoiceMode = settings.activation === "toggle" ? "strip" : settings.activation === "hold" ? "hud" : "pending";
      setMode(next);
      pressRef.current = next === "strip" ? null : { at: performance.now(), via };
      live.start();
    },
    [frozen, live, mode, settings.activation, stopAnd, gate],
  );

  const release = useCallback(() => {
    const p = pressRef.current;
    if (!p) return;
    pressRef.current = null;
    if (settings.activation === "auto" && performance.now() - p.at < TAP_MS) {
      setMode("strip");
      return;
    }
    stopAnd("insert");
  }, [settings.activation, stopAnd]);

  const cancel = useCallback(() => {
    pressRef.current = null;
    live.cancel();
  }, [live]);

  const polish = useCallback(() => {
    if (!offer) return;
    const target = targets.current.get(offer.id);
    const spec = target?.script;
    setPolishing(true);
    window.setTimeout(() => {
      if (target && spec) {
        const raw = offer.raw.trim();
        const polished = raw === spec.phrases.join("").trim() ? spec.polished : raw.replace(FILLERS, "");
        target.setDraft(appendDictation(offer.before, polished, lang));
        setLanded((m) => ({ ...m, [offer.id]: (m[offer.id] ?? 0) + 1 }));
      }
      setPolishing(false);
      setOffer(null);
    }, 900);
  }, [offer, lang]);

  // The shortcut: matched in the capture phase so Enter while recording sends
  // the recording instead of submitting the editor underneath.
  const keyState = useRef({ press, release, stopAnd, cancel, mode, phase, focusedId, primaryId, shortcut: settings.shortcut });
  keyState.current = { press, release, stopAnd, cancel, mode, phase, focusedId, primaryId, shortcut: settings.shortcut };
  useEffect(() => {
    if (frozen) return;
    const matches = (e: KeyboardEvent) => {
      const keys = keyState.current.shortcut;
      const code = keys[keys.length - 1] === "Space" ? "Space" : `Key${keys[keys.length - 1]}`;
      return (
        e.code === code &&
        e.altKey === keys.includes("⌥") &&
        (e.metaKey || e.ctrlKey) === keys.includes("⌘") &&
        e.shiftKey === keys.includes("⇧")
      );
    };
    const down = (e: KeyboardEvent) => {
      const k = keyState.current;
      const recording = k.phase === "listening" || k.phase === "requesting";
      if (matches(e)) {
        e.preventDefault();
        if (!e.repeat) k.press(k.focusedId ?? k.primaryId, "key");
      } else if (e.key === "Escape" && recording) {
        e.preventDefault();
        k.cancel();
      } else if (e.key === "Enter" && !e.shiftKey && k.phase === "listening" && k.mode === "strip") {
        e.preventDefault();
        e.stopPropagation();
        k.stopAnd("send");
      }
    };
    const up = (e: KeyboardEvent) => {
      if (e.code === "Space" || e.key === "Alt" || e.key === "Meta" || e.key === "Shift") keyState.current.release();
    };
    window.addEventListener("keydown", down, true);
    window.addEventListener("keyup", up, true);
    return () => {
      window.removeEventListener("keydown", down, true);
      window.removeEventListener("keyup", up, true);
    };
  }, [frozen]);

  const register = useCallback((spec: VoiceTargetSpec) => {
    targets.current.set(spec.id, spec);
    return () => {
      targets.current.delete(spec.id);
    };
  }, []);

  const value = useMemo<VoiceContextValue>(
    () => ({
      session,
      activeId: frozen ? frozen.activeId : activeId,
      mode: frozen?.mode ?? mode,
      focusedId,
      landed,
      offer: frozen?.offerFor ? { id: frozen.offerFor, before: "", raw: "" } : offer,
      polishing,
      register,
      focus: setFocusedId,
      press,
      release,
      stopAnd,
      cancel,
      retry: live.retry,
      polish,
      dismissOffer: () => setOffer(null),
    }),
    [session, frozen, activeId, mode, focusedId, landed, offer, polishing, register, press, release, stopAnd, cancel, live.retry, polish],
  );

  return <VoiceContext.Provider value={value}>{children}</VoiceContext.Provider>;
}

export function useVoiceLayer(): VoiceContextValue {
  const ctx = useContext(VoiceContext);
  if (!ctx) throw new Error("useVoiceLayer needs a VoiceProvider");
  return ctx;
}

/**
 * What a composer needs from the layer: register its draft, report focus, and
 * read whether it is the one being dictated into.
 */
export function useVoiceTarget(spec: VoiceTargetSpec) {
  const layer = useVoiceLayer();
  const specRef = useRef(spec);
  specRef.current = spec;
  const { register } = layer;
  useEffect(
    () =>
      register({
        id: spec.id,
        get script() {
          return specRef.current.script;
        },
        getDraft: () => specRef.current.getDraft(),
        setDraft: (v) => specRef.current.setDraft(v),
        send: (t) => specRef.current.send(t),
      }),
    [register, spec.id],
  );
  const active = layer.activeId === spec.id;
  return {
    id: spec.id,
    active,
    mode: layer.mode,
    session: layer.session,
    phase: active ? layer.session.phase : ("idle" as const),
    focused: layer.focusedId === spec.id,
    landed: layer.landed[spec.id] ?? 0,
    offered: layer.offer?.id === spec.id,
    layer,
    onFocus: () => layer.focus(spec.id),
  };
}

export type VoiceTarget = ReturnType<typeof useVoiceTarget>;
