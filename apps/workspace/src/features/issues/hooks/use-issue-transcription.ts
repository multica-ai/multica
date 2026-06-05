import { useCallback, useState } from "react";
import { api } from "@/shared/api";
import type { TranscriptionResponse } from "@/shared/types";

export type IssueTranscriptionStatus = "idle" | "transcribing" | "success" | "error";

export function useIssueTranscription() {
  const [status, setStatus] = useState<IssueTranscriptionStatus>("idle");
  const [result, setResult] = useState<TranscriptionResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  const transcribe = useCallback(async (file: File): Promise<TranscriptionResponse | null> => {
    setStatus("transcribing");
    setError(null);
    setResult(null);
    try {
      const response = await api.transcribeAudio(file);
      setResult(response);
      setStatus("success");
      return response;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Transcription failed");
      setStatus("error");
      return null;
    }
  }, []);

  const reset = useCallback(() => {
    setStatus("idle");
    setResult(null);
    setError(null);
  }, []);

  return { status, result, error, transcribe, reset };
}
