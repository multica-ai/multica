import { useCallback, useEffect, useId, useRef, useSyncExternalStore } from "react";
import { noteVoiceFocus, registerVoiceTarget, useVoiceState, type VoiceState } from "@multica/core/voice";
import { useVoiceModels } from "./engine";
import { voiceInputSupported } from "./recorder";

export interface ComposerVoiceOptions {
  /** Insert the transcript into the editor; false while it is not mounted. */
  insert: (text: string) => boolean;
  /** Send the composer; omit where Enter-to-send does not apply. */
  submit?: () => void;
  /** Mount a lazily-created editor. */
  activate?: () => void;
  /**
   * What the composer is writing to (a session and its agent, an issue).
   * When it changes under a mounted composer, dictation in flight is
   * cancelled so its text never lands in, or sends to, the new one.
   */
  scope?: string;
  /** Mounted but not on screen (a closed floating window): no dictation. */
  hidden?: boolean;
  disabled?: boolean;
}

export interface ComposerVoice {
  id: string;
  /** Show the mic: this app has a speech engine and can record. */
  available: boolean;
  /** The session, when this composer owns it. */
  session: VoiceState | null;
  /** Another composer is recording or transcribing. */
  busyElsewhere: boolean;
  /** The inline strip replaces the composer's toolbar row. */
  showStrip: boolean;
  /** The push-to-talk pill floats above the composer. */
  showHud: boolean;
  /** The mic is pressed, or held to talk: its button must stay where it is. */
  holding: boolean;
  /** No speech model is installed; the setup dialog is open for this composer. */
  showSetup: boolean;
  canSubmit: boolean;
  /** Spread on the composer root so the shortcut targets the last-used one. */
  onFocusCapture: () => void;
  /** Attach to the composer root so the shortcut skips it while it is off screen. */
  rootRef: (element: HTMLElement | null) => void;
}

const noSubscribe = () => () => {};

function onScreen(element: HTMLElement | null): boolean {
  if (!element) return true;
  if (!element.isConnected) return false;
  return typeof element.checkVisibility !== "function" || element.checkVisibility({ visibilityProperty: true });
}

export function useComposerVoice({
  insert,
  submit,
  activate,
  scope,
  hidden = false,
  disabled = false,
}: ComposerVoiceOptions): ComposerVoice {
  const id = useId();
  const engineAvailable = useVoiceModels()?.engineAvailable ?? false;
  const supported = useSyncExternalStore(noSubscribe, voiceInputSupported, () => false);
  const available = engineAvailable && supported && !disabled;
  const listening = available && !hidden;

  const callbacks = useRef({ insert, submit, activate });
  callbacks.current = { insert, submit, activate };
  const root = useRef<HTMLElement | null>(null);
  const rootRef = useCallback((element: HTMLElement | null) => {
    root.current = element;
  }, []);
  const canSubmit = submit !== undefined;

  useEffect(() => {
    if (!listening) return;
    return registerVoiceTarget(id, {
      insert: (text) => callbacks.current.insert(text),
      // Read at delivery: whether the composer can send may change mid-dictation.
      submit: () => callbacks.current.submit?.(),
      activate: () => callbacks.current.activate?.(),
      isShown: () => onScreen(root.current),
    });
  }, [listening, id, scope]);

  const state = useVoiceState();
  const session = listening && state.targetId === id ? state : null;
  const live =
    session?.phase === "requesting" ||
    session?.phase === "recording" ||
    session?.phase === "transcribing";
  return {
    id,
    available,
    session,
    busyElsewhere:
      state.targetId !== null && state.targetId !== id && state.phase !== "error" && state.phase !== "setup",
    showStrip: live && session?.mode === "strip",
    showHud: live && session?.mode === "hud",
    holding: live && session?.mode !== "strip",
    showSetup: session?.phase === "setup",
    canSubmit,
    onFocusCapture: () => noteVoiceFocus(id),
    rootRef,
  };
}
