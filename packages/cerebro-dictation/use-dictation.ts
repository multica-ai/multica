"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type {
  DictationError,
  DictationStatus,
  UseDictationOptions,
  UseDictationReturn,
  StreamingTranscriberSession,
} from "./types";

/**
 * Browser codec preference. Opus-in-WebM is the most widely supported
 * combination; the others fall back for Safari (WebKit added MediaRecorder
 * in 14.1 but with a smaller codec set). The first entry that the runtime
 * reports as supported wins.
 */
export const DEFAULT_MIME_TYPE_PREFERENCE: readonly string[] = [
  "audio/webm;codecs=opus",
  "audio/webm",
  "audio/ogg;codecs=opus",
  "audio/mp4",
];

const DEFAULT_MAX_DURATION_MS = 60_000;

/**
 * Timeslice for the one-shot (non-streaming) recorder. Without a timeslice the
 * browser only emits `dataavailable` at `stop()`, and iOS Safari/WebKit
 * reliably hands back an EMPTY blob in that case — recording visibly starts
 * (waveform + timer) but nothing is captured, so the transcribe fails with
 * "Recording failed" (Jesper, FIR-1637, iPhone Safari + Chrome). Flushing on an
 * interval forces WebKit to write real audio during the recording, so the blob
 * is populated by the time the user stops. Desktop Chrome is unaffected — the
 * streaming path already records with a 500ms timeslice without issue.
 */
const ONE_SHOT_TIMESLICE_MS = 1_000;

/**
 * Whether a failed transcribe is the engine cold-booting rather than a real
 * failure (FIR-2048). The HTTP transcriber throws a `TranscribeError` carrying
 * the backend status/code; the backend answers a cold start with 503
 * `warming_up`. Checked structurally (not via `instanceof`) so the hook stays
 * decoupled from the HTTP transcriber implementation.
 */
function isWarmingUpError(cause: unknown): boolean {
  if (!cause || typeof cause !== "object") return false;
  const e = cause as { status?: unknown; code?: unknown };
  return e.status === 503 || e.code === "warming_up";
}

/**
 * React hook wrapping the MediaRecorder lifecycle for dictation. The hook
 * is UI-agnostic: a `MicButton` (slice 2) renders state via `status` and
 * triggers `start()`/`stop()`, while consumers in arbitrary text inputs
 * read `lastTranscript` (or use `onTranscribed`) to insert the result.
 *
 * The transcribe function is injected so this package stays free of HTTP
 * concerns. Slice 3 can inject a streaming transcriber that talks to the
 * WebSocket proxy while keeping the one-shot transcriber path for tests and
 * fallback callers.
 */
export function useDictation(options: UseDictationOptions): UseDictationReturn {
  const {
    transcribe,
    streamTranscribe,
    mimeType,
    maxDurationMs = DEFAULT_MAX_DURATION_MS,
    onTranscribed,
    onPartial,
    onError,
  } = options;

  const [status, setStatus] = useState<DictationStatus>("idle");
  const [error, setError] = useState<DictationError | null>(null);
  const [lastTranscript, setLastTranscript] = useState<string | null>(null);
  // After a failed (one-shot) transcription we keep the recorded clip so the
  // user can retry without speaking again (FIR-2048). `canResend` drives the
  // "Re-send" button; the blob itself lives in a ref so it doesn't trigger
  // re-renders or get duplicated into state.
  const [canResend, setCanResend] = useState(false);
  const pendingAudioRef = useRef<Blob | null>(null);
  // Surfaced to the UI so a waveform can read the live mic input. Kept as
  // state (not just the ref below) so consumers re-render when it appears
  // and disappears.
  const [mediaStream, setMediaStream] = useState<MediaStream | null>(null);

  const recorderRef = useRef<MediaRecorder | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const stopTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const streamingSessionRef = useRef<StreamingTranscriberSession | null>(null);
  const streamingFinalRef = useRef<string | null>(null);
  const streamingErrorRef = useRef<DictationError | null>(null);
  const stopResolveRef = useRef<(() => void) | null>(null);
  const cancelledRef = useRef(false);
  // Forward reference so the max-duration timer in `start` can call the
  // latest `stop` without making `start` depend on it (and re-allocate
  // every render).
  const stopRef = useRef<() => Promise<void>>(() => Promise.resolve());

  // Refs for the latest callbacks so the start/stop closures don't capture
  // stale callers and force consumers to wrap them in useCallback.
  const transcribeRef = useRef(transcribe);
  const streamTranscribeRef = useRef(streamTranscribe);
  const onTranscribedRef = useRef(onTranscribed);
  const onPartialRef = useRef(onPartial);
  const onErrorRef = useRef(onError);
  useEffect(() => {
    transcribeRef.current = transcribe;
  }, [transcribe]);
  useEffect(() => {
    streamTranscribeRef.current = streamTranscribe;
  }, [streamTranscribe]);
  useEffect(() => {
    onTranscribedRef.current = onTranscribed;
  }, [onTranscribed]);
  useEffect(() => {
    onPartialRef.current = onPartial;
  }, [onPartial]);
  useEffect(() => {
    onErrorRef.current = onError;
  }, [onError]);

  const isSupported =
    typeof window !== "undefined" &&
    typeof window.MediaRecorder !== "undefined" &&
    typeof navigator !== "undefined" &&
    typeof navigator.mediaDevices?.getUserMedia === "function";

  const reportError = useCallback((next: DictationError) => {
    setError(next);
    setStatus("error");
    onErrorRef.current?.(next);
  }, []);

  const teardown = useCallback(() => {
    if (stopTimerRef.current !== null) {
      clearTimeout(stopTimerRef.current);
      stopTimerRef.current = null;
    }
    if (recorderRef.current && recorderRef.current.state !== "inactive") {
      try {
        recorderRef.current.stop();
      } catch {
        // recorder already torn down — safe to ignore
      }
    }
    recorderRef.current = null;
    if (streamRef.current) {
      for (const track of streamRef.current.getTracks()) {
        track.stop();
      }
      streamRef.current = null;
    }
    setMediaStream(null);
    chunksRef.current = [];
    streamingSessionRef.current = null;
    streamingFinalRef.current = null;
    stopResolveRef.current = null;
  }, []);

  // Best-effort cleanup if the consumer unmounts mid-recording.
  useEffect(() => {
    return () => {
      cancelledRef.current = true;
      abortRef.current?.abort();
      teardown();
    };
  }, [teardown]);

  const pickMimeType = useCallback(
    (preferred: string | undefined): string | undefined => {
      if (preferred) {
        return MediaRecorder.isTypeSupported(preferred) ? preferred : undefined;
      }
      for (const candidate of DEFAULT_MIME_TYPE_PREFERENCE) {
        if (MediaRecorder.isTypeSupported(candidate)) {
          return candidate;
        }
      }
      return undefined;
    },
    [],
  );

  const start = useCallback(async () => {
    if (status === "recording" || status === "transcribing") {
      return;
    }
    if (!isSupported) {
      reportError({
        kind: "unsupported",
        message:
          "This browser does not support MediaRecorder or microphone access.",
      });
      return;
    }

    setError(null);
    setLastTranscript(null);
    // A new recording supersedes any kept clip from a previous failure.
    pendingAudioRef.current = null;
    setCanResend(false);
    setStatus("requesting-permission");
    cancelledRef.current = false;
    streamingFinalRef.current = null;
    streamingErrorRef.current = null;

    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch (cause) {
      const denied =
        cause instanceof DOMException &&
        (cause.name === "NotAllowedError" || cause.name === "SecurityError");
      const missing =
        cause instanceof DOMException &&
        (cause.name === "NotFoundError" || cause.name === "OverconstrainedError");
      reportError({
        kind: denied
          ? "permission-denied"
          : missing
            ? "no-microphone"
            : "recording-failed",
        message:
          cause instanceof Error
            ? cause.message
            : "Could not access the microphone.",
        cause,
      });
      return;
    }

    if (cancelledRef.current) {
      for (const track of stream.getTracks()) track.stop();
      return;
    }

    streamRef.current = stream;
    setMediaStream(stream);
    chunksRef.current = [];

    const chosenMime = pickMimeType(mimeType);
    let recorder: MediaRecorder;
    try {
      recorder = chosenMime
        ? new MediaRecorder(stream, { mimeType: chosenMime })
        : new MediaRecorder(stream);
    } catch (cause) {
      teardown();
      reportError({
        kind: "recording-failed",
        message:
          cause instanceof Error
            ? cause.message
            : "Could not initialise the audio recorder.",
        cause,
      });
      return;
    }

    const controller = new AbortController();
    abortRef.current = controller;
    const streamingSession = streamTranscribeRef.current?.({
      mimeType: chosenMime || recorder.mimeType,
      signal: controller.signal,
      onPartial: (text) => {
        onPartialRef.current?.(text);
      },
      onFinal: (text) => {
        if (cancelledRef.current) return;
        streamingFinalRef.current = text;
        setLastTranscript(text);
        setStatus("idle");
        onTranscribedRef.current?.(text);
      },
      onError: (cause) => {
        if (cancelledRef.current) return;
        const nextError: DictationError = {
          kind: "streaming-failed",
          message: cause.message,
          cause,
        };
        streamingErrorRef.current = nextError;
        reportError(nextError);
        teardown();
      },
    });
    streamingSessionRef.current = streamingSession ?? null;

    recorderRef.current = recorder;
    recorder.addEventListener("dataavailable", (event) => {
      if (event.data && event.data.size > 0) {
        if (streamingSessionRef.current) {
          streamingSessionRef.current.sendAudio(event.data);
        } else {
          chunksRef.current.push(event.data);
        }
      }
    });
    recorder.addEventListener("error", (event) => {
      const cause = (event as Event & { error?: unknown }).error;
      reportError({
        kind: "recording-failed",
        message:
          cause instanceof Error ? cause.message : "Audio recorder error.",
        cause,
      });
      teardown();
    });
    recorder.addEventListener("stop", () => {
      const stopResolve = stopResolveRef.current;
      stopResolveRef.current = null;
      stopResolve?.();
    });

    try {
      recorder.start(streamingSession ? 500 : ONE_SHOT_TIMESLICE_MS);
    } catch (cause) {
      streamingSession?.cancel();
      teardown();
      reportError({
        kind: "recording-failed",
        message:
          cause instanceof Error
            ? cause.message
            : "Failed to start audio recorder.",
        cause,
      });
      return;
    }

    setStatus("recording");

    stopTimerRef.current = setTimeout(() => {
      void stopRef.current();
    }, maxDurationMs);
  }, [isSupported, maxDurationMs, mimeType, pickMimeType, reportError, status, teardown]);

  // Run the one-shot transcriber on a captured clip, shared by `stop` (first
  // attempt) and `resend` (retry the kept clip). On success it clears the kept
  // clip; on a non-aborted failure it keeps the clip and flags `canResend`,
  // tagging a cold start as "warming-up" so the UI can stay calm (FIR-2048).
  const runTranscribe = useCallback(
    async (audio: Blob) => {
      if (!transcribeRef.current) {
        reportError({
          kind: "transcription-failed",
          message: "Dictation transcriber is not configured.",
        });
        return;
      }
      cancelledRef.current = false;
      setStatus("transcribing");
      const controller = new AbortController();
      abortRef.current = controller;
      try {
        const text = await transcribeRef.current(audio, controller.signal);
        if (cancelledRef.current) {
          return;
        }
        pendingAudioRef.current = null;
        setCanResend(false);
        setLastTranscript(text);
        setStatus("idle");
        onTranscribedRef.current?.(text);
      } catch (cause) {
        if (controller.signal.aborted) {
          reportError({
            kind: "aborted",
            message: "Transcription was cancelled.",
            cause,
          });
          return;
        }
        // Keep the clip so the user can retry without re-recording (FIR-2048).
        pendingAudioRef.current = audio;
        setCanResend(true);
        reportError({
          kind: isWarmingUpError(cause) ? "warming-up" : "transcription-failed",
          message:
            cause instanceof Error ? cause.message : "Transcription failed.",
          cause,
        });
      } finally {
        abortRef.current = null;
      }
    },
    [reportError],
  );

  const stop = useCallback(async () => {
    const recorder = recorderRef.current;
    if (!recorder || recorder.state === "inactive") {
      return;
    }
    if (stopTimerRef.current !== null) {
      clearTimeout(stopTimerRef.current);
      stopTimerRef.current = null;
    }

    setStatus("transcribing");
    const streamingSession = streamingSessionRef.current;

    await new Promise<void>((resolve) => {
      stopResolveRef.current = resolve;
      try {
        recorder.requestData?.();
        recorder.stop();
      } catch {
        resolve();
      }
    });

    if (cancelledRef.current) {
      teardown();
      return;
    }

    if (streamingSession) {
      streamingSession.endUtterance();
      let hasStreamingFinal = false;
      try {
        await streamingSession.finished;
        hasStreamingFinal = Boolean(streamingFinalRef.current);
      } catch (cause) {
        if (cancelledRef.current) return;
        if (streamingErrorRef.current) return;
        reportError({
          kind: "streaming-failed",
          message: cause instanceof Error ? cause.message : "Dictation stream failed.",
          cause,
        });
        return;
      } finally {
        abortRef.current = null;
        teardown();
      }
      if (!hasStreamingFinal && !cancelledRef.current) {
        setStatus("idle");
      }
      return;
    }

    const chunks = chunksRef.current;
    const audio = new Blob(chunks, {
      type: chunks[0]?.type || recorder.mimeType || "audio/webm",
    });
    teardown();

    if (audio.size === 0) {
      // Carry the format + chunk count so an empty capture on a device we
      // can't reproduce (e.g. iPhone) is diagnosable from the toast alone.
      const fmt = chunks[0]?.type || recorder.mimeType || "unknown";
      reportError({
        kind: "recording-failed",
        message: `no audio captured (${fmt}, ${chunks.length} chunk(s))`,
      });
      return;
    }

    await runTranscribe(audio);
  }, [reportError, runTranscribe, teardown]);

  // Retry the kept clip from the last failure without re-recording (FIR-2048).
  // No-op when there is nothing kept or a recording / transcription is already
  // in flight (`abortRef` is held during both).
  const resend = useCallback(async () => {
    const audio = pendingAudioRef.current;
    if (!audio || abortRef.current) {
      return;
    }
    await runTranscribe(audio);
  }, [runTranscribe]);

  const cancel = useCallback(() => {
    cancelledRef.current = true;
    streamingErrorRef.current = null;
    abortRef.current?.abort();
    streamingSessionRef.current?.cancel();
    teardown();
    // Cancelling discards the kept clip too — the user chose to abandon it.
    pendingAudioRef.current = null;
    setCanResend(false);
    setStatus("idle");
  }, [teardown]);

  useEffect(() => {
    stopRef.current = stop;
  }, [stop]);

  return {
    status,
    error,
    lastTranscript,
    isSupported,
    mediaStream,
    start,
    stop,
    cancel,
    canResend,
    resend,
  };
}
