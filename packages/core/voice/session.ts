"use client";

/**
 * The single voice-input session shared by every composer.
 *
 * Composers (chat, issue comment, reply) register as targets; one recording
 * exists at a time and its text goes to the composer it was started from. A
 * tap starts the inline strip and a second tap inserts; holding (button or
 * shortcut) shows the floating HUD and releasing inserts. Speech is
 * transcribed on this device; with no model installed, a press opens the
 * setup dialog instead of recording.
 *
 * Recording and transcription are platform work behind `VoiceRuntime`. The
 * app connects one (Desktop's on-device engine); without it nothing records.
 */
import { create } from "zustand";
import { getShortcut } from "../shortcuts/store";
import { shortcutMatchesEvent } from "../shortcuts/definitions";
import { activeVoiceModel, useVoiceModelsStore, type VoiceModel, type VoiceModels, type VoiceTranscription } from "./models";
import { voiceHistoryDays, type VoiceHistoryStatus } from "./history";
import { voiceHotwords } from "./hotwords";
import { useVoiceSettingsStore } from "./store";
import { applyVocabulary, removeSpokenFillers } from "./text";

export type VoiceRecorderFailure = "permission-denied" | "no-microphone" | "failed";

export class VoiceRecorderError extends Error {
  constructor(readonly failure: VoiceRecorderFailure) {
    super(failure);
    this.name = "VoiceRecorderError";
  }
}

export interface VoiceMeter {
  levels: number[];
  elapsedMs: number;
}

export interface VoiceClip {
  blob: Blob;
  durationMs: number;
  /** False when the clip is too short or never rose above background noise. */
  heardSpeech: boolean;
}

export interface VoiceRecorder {
  /** Resolves once audio is flowing; rejects with a VoiceRecorderError. */
  start: (deviceId: string | null) => Promise<void>;
  finish: () => Promise<VoiceClip>;
  discard: () => void;
}

export interface VoiceDecodeOptions {
  /** A VOICE_LANGUAGES value. */
  language: string;
  /** Names and terms to favour, for models that take them. */
  hotwords: string[];
}

export interface VoiceRuntime {
  /** `onLimit` fires when the recording hits its length cap. */
  record: (onMeter: (meter: VoiceMeter) => void, onLimit: () => void) => VoiceRecorder;
  /** Load the model while the user is still talking. */
  prepare: (model: VoiceModel, options: VoiceDecodeOptions) => void;
  transcribe: (clip: Blob, model: VoiceModel, options: VoiceDecodeOptions) => Promise<VoiceTranscription>;
  /** Report the model list now and whenever it changes. */
  watchModels: (onChange: (models: VoiceModels) => void) => void;
  /** Keep recordings on this device; absent where there is nowhere to keep them. */
  history?: VoiceHistoryRuntime;
}

export interface VoiceHistoryRuntime {
  /** Resolves to the entry id, or null when it could not be kept. */
  save: (clip: VoiceClip, keepDays: number | null) => Promise<string | null>;
  update: (id: string, status: VoiceHistoryStatus, text?: string) => void;
}

/** `setup`: no speech model is installed yet, so the setup dialog is open. */
export type VoicePhase = "idle" | "setup" | "requesting" | "recording" | "transcribing" | "error";
/** `pending` is a press in "auto" activation that is not yet a tap or a hold. */
export type VoiceMode = "pending" | "strip" | "hud";
export type VoiceErrorKind = VoiceRecorderFailure | "no-speech" | "unavailable" | "transcription-failed";

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
  /** False while the composer is mounted but off screen; the shortcut skips it. */
  isShown?: () => boolean;
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
const EMPTY_METER: VoiceMeter = { levels: [], elapsedMs: 0 };

interface VoiceSessionState {
  session: VoiceState;
  /** Level history and elapsed time; changes ~20 times a second while recording. */
  meter: VoiceMeter;
}

export const useVoiceSessionStore = create<VoiceSessionState>()(() => ({
  session: IDLE,
  meter: EMPTY_METER,
}));

let runtime: VoiceRuntime | null = null;
let runtimeConnected = false;

const targets = new Map<string, VoiceTarget>();
let focusedId: string | null = null;

let session = 0;
let recording: VoiceRecorder | null = null;
let lastClip: Blob | null = null;
/** The history entry for `lastClip`, and whether its outcome is still open. */
let historyEntry: Promise<string | null> | null = null;
let historyOpen = false;
let sendAfterInsert = false;
let held = false;
let heldByKey = false;
let holdTimer: ReturnType<typeof setTimeout> | null = null;

/** Connect the platform's recorder and engine; later calls are no-ops. */
export function connectVoiceRuntime(factory: () => VoiceRuntime | null) {
  if (runtimeConnected) return;
  runtimeConnected = true;
  runtime = factory();
  runtime?.watchModels((models) => useVoiceModelsStore.getState().setModels(models));
}

function state(): VoiceState {
  return useVoiceSessionStore.getState().session;
}

function setState(patch: Partial<VoiceState>) {
  useVoiceSessionStore.setState((current) => ({ session: { ...current.session, ...patch } }));
}

function setMeter(meter: VoiceMeter) {
  useVoiceSessionStore.setState({ meter });
}

function isCapturing() {
  const { phase } = state();
  return phase === "requesting" || phase === "recording";
}

function decodeOptions(): VoiceDecodeOptions {
  const { language, vocabulary } = useVoiceSettingsStore.getState();
  return { language, hotwords: voiceHotwords(vocabulary) };
}

function keepInHistory(clip: VoiceClip) {
  const keepDays = voiceHistoryDays(useVoiceSettingsStore.getState().keepHistory);
  const history = runtime?.history;
  historyEntry = history && keepDays !== 0 ? history.save(clip, keepDays).catch(() => null) : null;
  historyOpen = historyEntry !== null;
}

function settleHistory(status: VoiceHistoryStatus, text?: string) {
  const entry = historyEntry;
  const history = runtime?.history;
  historyOpen = status === "transcribing";
  if (!entry || !history) return;
  void entry.then((id) => {
    if (id) history.update(id, status, text);
  });
}

/** The transcript as it is inserted: fillers dropped, vocabulary spelled. */
function cleanTranscript(text: string): string {
  const settings = useVoiceSettingsStore.getState();
  const spoken = settings.removeFillers ? removeSpokenFillers(text) : text;
  return applyVocabulary(spoken, settings.vocabulary).trim();
}

/**
 * Transcribe a kept recording again, outside any composer (history). Null
 * when there is no model or engine, or nothing was heard.
 */
export async function transcribeVoiceClip(clip: Blob): Promise<string | null> {
  const model = activeVoiceModel();
  if (!runtime || !model) return null;
  const result = await runtime.transcribe(clip, model, decodeOptions()).catch(() => null);
  if (!result?.ok) return null;
  return cleanTranscript(result.text) || null;
}

function clearHold() {
  held = false;
  heldByKey = false;
  if (holdTimer !== null) clearTimeout(holdTimer);
  holdTimer = null;
}

function begin(targetId: string, mode: VoiceMode) {
  const target = targets.get(targetId);
  if (!target || !runtime) return;
  const model = activeVoiceModel();
  if (!model) {
    openSetup(targetId);
    return;
  }
  target.activate?.();
  runtime.prepare(model, decodeOptions());
  const token = ++session;
  recording?.discard();
  lastClip = null;
  historyEntry = null;
  historyOpen = false;
  sendAfterInsert = false;
  const current = runtime.record(
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
      fail(err instanceof VoiceRecorderError ? err.failure : "failed", false);
    },
  );
}

async function finish(send: boolean) {
  const current = recording;
  if (!current || !isCapturing()) return;
  if (state().phase === "requesting") {
    // Released while the permission prompt is still open: keep going as a
    // tap so the user can answer the prompt and then speak.
    if (state().mode !== "strip") setState({ mode: "strip" });
    return;
  }
  recording = null;
  clearHold();
  sendAfterInsert = send;
  const token = session;
  setState({ phase: "transcribing", mode: state().mode === "pending" ? "strip" : state().mode });
  const clip = await current.finish();
  if (token !== session) return;
  if (!clip.heardSpeech) {
    // An accidental tap of a hold-to-talk control is not worth an error.
    if (clip.durationMs < HOLD_MS) cancelVoice();
    else fail("no-speech", false);
    return;
  }
  lastClip = clip.blob;
  keepInHistory(clip);
  await transcribe(token);
}

async function transcribe(token: number) {
  const clip = lastClip;
  const model = activeVoiceModel();
  if (!clip || !runtime) return;
  const { targetId } = state();
  if (!model) {
    settleHistory("failed");
    if (targetId) openSetup(targetId);
    return;
  }
  const result = await runtime.transcribe(clip, model, decodeOptions()).catch(() => null);
  if (token !== session) return;
  if (!result?.ok) {
    settleHistory("failed");
    if (result?.reason === "model-missing" && targetId) openSetup(targetId);
    else if (result?.reason === "engine-unavailable") fail("unavailable", false);
    else fail("transcription-failed", true);
    return;
  }
  const cleaned = cleanTranscript(result.text);
  if (!cleaned) {
    settleHistory("failed");
    fail("no-speech", false);
    return;
  }
  // Kept before inserting: the text survives a composer that went away.
  settleHistory("done", cleaned);
  deliver(token, cleaned);
}

function deliver(token: number, text: string, attempt = 0) {
  if (token !== session) return;
  const { targetId } = state();
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
  // Cancelled while transcribing: the recording stays in history to retry.
  if (historyOpen) settleHistory("interrupted");
  historyEntry = null;
  historyOpen = false;
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
  const { phase, targetId, canRetry } = state();
  if (phase !== "error" || !targetId) return;
  if (canRetry && lastClip) {
    if (historyEntry) settleHistory("transcribing");
    const token = ++session;
    setState({ phase: "transcribing", error: null, canRetry: false });
    void transcribe(token);
    return;
  }
  begin(targetId, "strip");
}

/** Stop recording and insert (or insert and send). */
export function stopVoice(send = false) {
  void finish(send);
}

/** Mic button or shortcut pressed for `targetId`. */
export function pressVoice(targetId: string) {
  const current = state();
  if (current.phase === "setup") return;
  if (current.targetId !== null && current.targetId !== targetId && current.phase !== "error") return;
  if (isCapturing()) {
    // A second tap ends a tap-started recording.
    if (current.mode === "strip" && !held) void finish(false);
    return;
  }
  if (current.phase === "transcribing") return;
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
    if (held && isCapturing() && state().mode === "pending") setState({ mode: "hud" });
  }, HOLD_MS);
}

/** Mic button or shortcut released. */
export function releaseVoice() {
  if (!held) return;
  const wasHolding = state().mode === "hud";
  clearHold();
  if (!isCapturing()) return;
  if (wasHolding) void finish(false);
  else if (state().mode === "pending") setState({ mode: "strip" });
}

/** A press that cannot be held (keyboard activation, or starting from setup). */
export function toggleVoice(targetId: string) {
  const current = state();
  const free = current.phase === "error" || (current.phase === "setup" && current.targetId === targetId);
  if (current.targetId !== null && current.targetId !== targetId && !free) return;
  if (isCapturing()) {
    void finish(false);
    return;
  }
  if (current.phase === "transcribing") return;
  begin(targetId, "strip");
}

export function noteVoiceFocus(targetId: string) {
  focusedId = targetId;
}

function isShown(id: string): boolean {
  return targets.get(id)?.isShown?.() ?? true;
}

/** The composer focused last if it is on screen, else the newest one that is. */
function pickTarget(): string | null {
  if (focusedId && targets.has(focusedId) && isShown(focusedId)) return focusedId;
  return [...targets.keys()].reverse().find(isShown) ?? null;
}

function onKeyDown(event: KeyboardEvent) {
  const current = state();
  if (event.key === "Escape" && (isCapturing() || current.phase === "transcribing")) {
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
      current.mode === "strip" &&
      current.phase === "recording"
    ) {
      event.preventDefault();
      event.stopPropagation();
      void finish(Boolean(current.targetId && targets.get(current.targetId)?.submit));
      return;
    }
  }
  if (!shortcutMatchesEvent(getShortcut("voiceInput"), event)) return;
  event.preventDefault();
  event.stopPropagation();
  if (event.repeat) return;
  const targetId = isCapturing() ? current.targetId : pickTarget();
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

/**
 * Register a composer; returns the unregister function. Unregistering the
 * composer that owns the session cancels it, so a transcript still on its
 * way is dropped rather than delivered.
 */
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
    if (state().targetId === id) cancelVoice();
    if (targets.size === 0) {
      window.removeEventListener("keydown", onKeyDown, true);
      window.removeEventListener("keyup", onKeyUp, true);
      window.removeEventListener("blur", onWindowBlur);
    }
  };
}

export function useVoiceState(): VoiceState {
  return useVoiceSessionStore((s) => s.session);
}

export function useVoiceMeter(): VoiceMeter {
  return useVoiceSessionStore((s) => s.meter);
}

export function getVoiceState(): VoiceState {
  return state();
}
