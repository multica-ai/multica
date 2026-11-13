/**
 * The single voice-input session shared by every composer.
 *
 * Composers (chat, issue comment, reply) register as targets; one recording
 * exists at a time and its text goes to the composer it was started from. A
 * tap starts the inline strip and a second tap inserts; holding (button or
 * shortcut) shows the floating HUD and releasing inserts. Module state rather
 * than a React provider so a composer works wherever it is mounted. Speech
 * is transcribed on this device; with no model installed, a press opens the
 * setup dialog instead of recording.
 */
import { useSyncExternalStore } from "react";
import { getShortcut, shortcutMatchesEvent } from "@multica/core/shortcuts";
import {
  applyVocabulary,
  removeSpokenFillers,
  useVoiceSettingsStore,
} from "@multica/core/voice";
import { activeModel, prepareModel, transcribeClip } from "./engine";
import { type Meter, Recording, RecorderError, type RecorderFailure } from "./recorder";

/** `setup`: no speech model is installed yet, so the setup dialog is open. */
export type VoicePhase = "idle" | "setup" | "requesting" | "recording" | "transcribing" | "error";
/** `pending` is a press in "auto" activation that is not yet a tap or a hold. */
export type VoiceMode = "pending" | "strip" | "hud";
export type VoiceErrorKind = RecorderFailure | "no-speech" | "unavailable" | "transcription-failed";

export interface VoiceState {
  targetId: string | null;
  phase: VoicePhase;
  mode: VoiceMode;
  error: VoiceErrorKind | null;
  /** The failed clip is still held and can be sent again. */
  canRetry: boolean;
}

export interface VoiceTarget {
  /** Insert the transcript; false while the editor is not mounted yet. */
  insert: (text: string) => boolean;
  /** Send the composer after inserting (Enter while dictating). */
  submit?: () => void;
  /** Mount a lazily-created editor so the transcript has somewhere to go. */
  activate?: () => void;
}

/** A press shorter than this is a tap; longer is push-to-talk. */
const HOLD_MS = 250;
const INSERT_ATTEMPTS = 20;

const IDLE: VoiceState = {
  targetId: null,
  phase: "idle",
  mode: "strip",
  error: null,
  canRetry: false,
};
const EMPTY_METER: Meter = { levels: [], elapsedMs: 0 };

let state: VoiceState = IDLE;
let meter: Meter = EMPTY_METER;
const stateListeners = new Set<() => void>();
const meterListeners = new Set<() => void>();

const targets = new Map<string, VoiceTarget>();
let focusedId: string | null = null;

let session = 0;
let recording: Recording | null = null;
let lastClip: Blob | null = null;
let sendAfterInsert = false;
let held = false;
let heldByKey = false;
let holdTimer: ReturnType<typeof setTimeout> | null = null;

function setState(patch: Partial<VoiceState>) {
  state = { ...state, ...patch };
  stateListeners.forEach((listener) => listener());
}

function setMeter(next: Meter) {
  meter = next;
  meterListeners.forEach((listener) => listener());
}

function isCapturing() {
  return state.phase === "requesting" || state.phase === "recording";
}

function clearHold() {
  held = false;
  heldByKey = false;
  if (holdTimer !== null) clearTimeout(holdTimer);
  holdTimer = null;
}

function begin(targetId: string, mode: VoiceMode) {
  const target = targets.get(targetId);
  if (!target) return;
  const model = activeModel();
  if (!model) {
    openSetup(targetId);
    return;
  }
  target.activate?.();
  prepareModel(model);
  const token = ++session;
  recording?.discard();
  lastClip = null;
  sendAfterInsert = false;
  const current = new Recording(
    (next) => {
      if (token === session) setMeter(next);
    },
    () => {
      if (token === session) void finish(false);
    },
  );
  recording = current;
  setMeter(EMPTY_METER);
  setState({ targetId, phase: "requesting", mode, error: null, canRetry: false });
  current.start(useVoiceSettingsStore.getState().deviceId).then(
    () => {
      if (token === session) setState({ phase: "recording" });
    },
    (err: unknown) => {
      if (token !== session) return;
      recording = null;
      clearHold();
      fail(err instanceof RecorderError ? err.failure : "failed", false);
    },
  );
}

async function finish(send: boolean) {
  const current = recording;
  if (!current || !isCapturing()) return;
  if (state.phase === "requesting") {
    // Released while the permission prompt is still open: keep going as a
    // tap so the user can answer the prompt and then speak.
    if (state.mode !== "strip") setState({ mode: "strip" });
    return;
  }
  recording = null;
  clearHold();
  sendAfterInsert = send;
  const token = session;
  setState({ phase: "transcribing", mode: state.mode === "pending" ? "strip" : state.mode });
  const clip = await current.finish();
  if (token !== session) return;
  if (!clip.heardSpeech) {
    // An accidental tap of a hold-to-talk control is not worth an error.
    if (clip.durationMs < HOLD_MS) cancelVoice();
    else fail("no-speech", false);
    return;
  }
  lastClip = clip.blob;
  await transcribe(token);
}

async function transcribe(token: number) {
  const clip = lastClip;
  const model = activeModel();
  if (!clip) return;
  if (!model) {
    if (state.targetId) openSetup(state.targetId);
    return;
  }
  const result = await transcribeClip(clip, model).catch(() => null);
  if (token !== session) return;
  if (!result?.ok) {
    if (result?.reason === "model-missing" && state.targetId) openSetup(state.targetId);
    else if (result?.reason === "engine-unavailable") fail("unavailable", false);
    else fail("transcription-failed", true);
    return;
  }
  const settings = useVoiceSettingsStore.getState();
  const spoken = settings.removeFillers ? removeSpokenFillers(result.text) : result.text;
  const cleaned = applyVocabulary(spoken, settings.vocabulary).trim();
  if (!cleaned) {
    fail("no-speech", false);
    return;
  }
  deliver(token, cleaned);
}

function deliver(token: number, text: string, attempt = 0) {
  if (token !== session) return;
  const targetId = state.targetId;
  const target = targetId ? targets.get(targetId) : undefined;
  if (!target) {
    // The composer went away (navigation) before the text arrived.
    cancelVoice();
    return;
  }
  if (!target.insert(text)) {
    // A lazily-mounted editor may still be coming up.
    if (attempt < INSERT_ATTEMPTS) {
      setTimeout(() => deliver(token, text, attempt + 1), 100);
    } else {
      fail("transcription-failed", true);
    }
    return;
  }
  const send = sendAfterInsert;
  lastClip = null;
  sendAfterInsert = false;
  setState(IDLE);
  if (send) target.submit?.();
}

function openSetup(targetId: string) {
  session++;
  recording?.discard();
  recording = null;
  clearHold();
  setMeter(EMPTY_METER);
  setState({ targetId, phase: "setup", mode: "strip", error: null, canRetry: false });
}

function fail(error: VoiceErrorKind, canRetry: boolean) {
  setMeter(EMPTY_METER);
  setState({ phase: "error", mode: "strip", error, canRetry: canRetry && lastClip !== null });
}

/** Abandon whatever is in flight, including a failed clip. */
export function cancelVoice() {
  session++;
  recording?.discard();
  recording = null;
  lastClip = null;
  sendAfterInsert = false;
  clearHold();
  setMeter(EMPTY_METER);
  setState(IDLE);
}

export function retryVoice() {
  if (state.phase !== "error" || !state.targetId) return;
  if (state.canRetry && lastClip) {
    const token = ++session;
    setState({ phase: "transcribing", error: null, canRetry: false });
    void transcribe(token);
    return;
  }
  begin(state.targetId, "strip");
}

/** Stop recording and insert (or insert and send). */
export function stopVoice(send = false) {
  void finish(send);
}

/** Mic button or shortcut pressed for `targetId`. */
export function pressVoice(targetId: string) {
  if (state.phase === "setup") return;
  if (state.targetId !== null && state.targetId !== targetId && state.phase !== "error") return;
  if (isCapturing()) {
    // A second tap ends a tap-started recording.
    if (state.mode === "strip" && !held) void finish(false);
    return;
  }
  if (state.phase === "transcribing") return;
  const { activation } = useVoiceSettingsStore.getState();
  held = true;
  if (activation === "hold") {
    begin(targetId, "hud");
    return;
  }
  if (activation === "toggle") {
    begin(targetId, "strip");
    return;
  }
  begin(targetId, "pending");
  holdTimer = setTimeout(() => {
    holdTimer = null;
    if (held && isCapturing() && state.mode === "pending") setState({ mode: "hud" });
  }, HOLD_MS);
}

/** Mic button or shortcut released. */
export function releaseVoice() {
  if (!held) return;
  const wasHolding = state.mode === "hud";
  clearHold();
  if (!isCapturing()) return;
  if (wasHolding) void finish(false);
  else if (state.mode === "pending") setState({ mode: "strip" });
}

/** A press that cannot be held (keyboard activation, or starting from setup). */
export function toggleVoice(targetId: string) {
  const free = state.phase === "error" || (state.phase === "setup" && state.targetId === targetId);
  if (state.targetId !== null && state.targetId !== targetId && !free) return;
  if (isCapturing()) {
    void finish(false);
    return;
  }
  if (state.phase === "transcribing") return;
  begin(targetId, "strip");
}

export function noteVoiceFocus(targetId: string) {
  focusedId = targetId;
}

function pickTarget(): string | null {
  if (focusedId && targets.has(focusedId)) return focusedId;
  let last: string | null = null;
  for (const id of targets.keys()) last = id;
  return last;
}

function onKeyDown(event: KeyboardEvent) {
  if (event.key === "Escape" && (isCapturing() || state.phase === "transcribing")) {
    event.preventDefault();
    event.stopPropagation();
    cancelVoice();
    return;
  }
  if (isCapturing() && !heldByKey) {
    if (
      event.key === "Enter" &&
      !event.shiftKey &&
      !event.isComposing &&
      state.mode === "strip" &&
      state.phase === "recording"
    ) {
      event.preventDefault();
      event.stopPropagation();
      void finish(Boolean(state.targetId && targets.get(state.targetId)?.submit));
      return;
    }
  }
  if (!shortcutMatchesEvent(getShortcut("voiceInput"), event)) return;
  event.preventDefault();
  event.stopPropagation();
  if (event.repeat) return;
  const targetId = isCapturing() ? state.targetId : pickTarget();
  if (!targetId) return;
  pressVoice(targetId);
  if (held) heldByKey = true;
}

function onKeyUp() {
  // Releasing either key of the chord ends a push-to-talk hold.
  if (heldByKey) releaseVoice();
}

function onWindowBlur() {
  if (held) releaseVoice();
}

/** Register a composer; returns the unregister function. */
export function registerVoiceTarget(id: string, target: VoiceTarget): () => void {
  if (targets.size === 0) {
    window.addEventListener("keydown", onKeyDown, true);
    window.addEventListener("keyup", onKeyUp, true);
    window.addEventListener("blur", onWindowBlur);
  }
  targets.set(id, target);
  return () => {
    if (targets.get(id) !== target) return;
    targets.delete(id);
    if (focusedId === id) focusedId = null;
    if (state.targetId === id) cancelVoice();
    if (targets.size === 0) {
      window.removeEventListener("keydown", onKeyDown, true);
      window.removeEventListener("keyup", onKeyUp, true);
      window.removeEventListener("blur", onWindowBlur);
    }
  };
}

function subscribeState(listener: () => void) {
  stateListeners.add(listener);
  return () => {
    stateListeners.delete(listener);
  };
}

function subscribeMeter(listener: () => void) {
  meterListeners.add(listener);
  return () => {
    meterListeners.delete(listener);
  };
}

export function useVoiceState(): VoiceState {
  return useSyncExternalStore(subscribeState, () => state, () => IDLE);
}

/** Level history and elapsed time; changes ~20 times a second while recording. */
export function useVoiceMeter(): Meter {
  return useSyncExternalStore(subscribeMeter, () => meter, () => EMPTY_METER);
}

export function getVoiceState(): VoiceState {
  return state;
}
