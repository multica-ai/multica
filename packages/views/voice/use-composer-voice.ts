import { useEffect, useId, useRef, useSyncExternalStore } from "react";
import { useVoiceModels } from "./engine";
import { voiceInputSupported } from "./recorder";
import {
  noteVoiceFocus,
  registerVoiceTarget,
  useVoiceState,
  type VoiceState,
} from "./voice-layer";

export interface ComposerVoiceOptions {
  /** Insert the transcript into the editor; false while it is not mounted. */
  insert: (text: string) => boolean;
  /** Send the composer; omit where Enter-to-send does not apply. */
  submit?: () => void;
  /** Mount a lazily-created editor. */
  activate?: () => void;
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
  /** No speech model is installed; the setup dialog is open for this composer. */
  showSetup: boolean;
  canSubmit: boolean;
  /** Spread on the composer root so the shortcut targets the last-used one. */
  onFocusCapture: () => void;
}

const noSubscribe = () => () => {};

export function useComposerVoice({
  insert,
  submit,
  activate,
  disabled = false,
}: ComposerVoiceOptions): ComposerVoice {
  const id = useId();
  const engineAvailable = useVoiceModels()?.engineAvailable ?? false;
  const supported = useSyncExternalStore(noSubscribe, voiceInputSupported, () => false);
  const available = engineAvailable && supported && !disabled;

  const callbacks = useRef({ insert, submit, activate });
  callbacks.current = { insert, submit, activate };
  const canSubmit = submit !== undefined;

  useEffect(() => {
    if (!available) return;
    return registerVoiceTarget(id, {
      insert: (text) => callbacks.current.insert(text),
      submit: canSubmit ? () => callbacks.current.submit?.() : undefined,
      activate: () => callbacks.current.activate?.(),
    });
  }, [available, canSubmit, id]);

  const state = useVoiceState();
  const session = available && state.targetId === id ? state : null;
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
    showSetup: session?.phase === "setup",
    canSubmit,
    onFocusCapture: () => noteVoiceFocus(id),
  };
}
