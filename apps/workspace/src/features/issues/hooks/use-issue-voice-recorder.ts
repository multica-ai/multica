import { useCallback, useMemo, useRef, useState } from "react";

export type IssueVoiceRecorderStatus = "idle" | "requesting-permission" | "recording" | "error" | "unsupported";

export interface RecordedVoice {
  blob: Blob;
  file: File;
  durationMs: number;
  mimeType: string;
}

const MIME_CANDIDATES = ["audio/webm", "audio/mp4", "audio/ogg", "audio/wav"];

function hasRecordingSupport(): boolean {
  return typeof navigator !== "undefined"
    && !!navigator.mediaDevices?.getUserMedia
    && typeof MediaRecorder !== "undefined";
}

function selectMimeType(): string {
  if (typeof MediaRecorder === "undefined" || !MediaRecorder.isTypeSupported) {
    return "";
  }
  return MIME_CANDIDATES.find((type) => MediaRecorder.isTypeSupported(type)) ?? "";
}

function extensionForMimeType(mimeType: string): string {
  if (mimeType.includes("mp4")) return "m4a";
  if (mimeType.includes("ogg")) return "ogg";
  if (mimeType.includes("wav")) return "wav";
  return "webm";
}

export function useIssueVoiceRecorder() {
  const [status, setStatus] = useState<IssueVoiceRecorderStatus>(() => (hasRecordingSupport() ? "idle" : "unsupported"));
  const [recording, setRecording] = useState<RecordedVoice | null>(null);
  const [error, setError] = useState<string | null>(null);
  const recorderRef = useRef<MediaRecorder | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const startedAtRef = useRef(0);

  const supported = useMemo(() => hasRecordingSupport(), []);

  const cleanupStream = useCallback(() => {
    streamRef.current?.getTracks().forEach((track) => track.stop());
    streamRef.current = null;
  }, []);

  const start = useCallback(async () => {
    if (!hasRecordingSupport()) {
      setStatus("unsupported");
      setError("This browser does not support recording");
      return;
    }

    setStatus("requesting-permission");
    setError(null);
    setRecording(null);
    chunksRef.current = [];

    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const mimeType = selectMimeType();
      const recorder = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream);
      streamRef.current = stream;
      recorderRef.current = recorder;
      startedAtRef.current = Date.now();

      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) chunksRef.current.push(event.data);
      };
      recorder.onerror = () => {
        setStatus("error");
        setError("Recording failed");
        cleanupStream();
      };
      recorder.onstop = () => {
        const resolvedMimeType = recorder.mimeType || mimeType || "audio/webm";
        const blob = new Blob(chunksRef.current, { type: resolvedMimeType });
        const file = new File(
          [blob],
          `voice-${Date.now()}.${extensionForMimeType(resolvedMimeType)}`,
          { type: resolvedMimeType },
        );
        setRecording({
          blob,
          file,
          durationMs: Math.max(0, Date.now() - startedAtRef.current),
          mimeType: resolvedMimeType,
        });
        setStatus("idle");
        cleanupStream();
      };

      recorder.start();
      setStatus("recording");
    } catch (err) {
      setStatus("error");
      setError(err instanceof Error ? err.message : "Microphone permission was denied");
      cleanupStream();
    }
  }, [cleanupStream]);

  const stop = useCallback(() => {
    const recorder = recorderRef.current;
    if (!recorder || recorder.state === "inactive") return;
    recorder.stop();
  }, []);

  const reset = useCallback(() => {
    if (recorderRef.current && recorderRef.current.state !== "inactive") {
      recorderRef.current.stop();
    }
    chunksRef.current = [];
    recorderRef.current = null;
    setRecording(null);
    setError(null);
    setStatus(hasRecordingSupport() ? "idle" : "unsupported");
    cleanupStream();
  }, [cleanupStream]);

  return { status, supported, recording, error, start, stop, reset };
}
