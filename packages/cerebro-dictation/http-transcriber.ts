import type { Transcriber } from "./types";

/**
 * One-shot HTTP transcriber: record an utterance, POST it as multipart `file`
 * to the cerebro dictation transcribe endpoint, get the text back. This is the
 * non-streaming path used by the chat, notes, and document editors — the
 * backend proxies the clip to the cerebro-inference hviske model and returns
 * `{ text }`.
 *
 * `url` is the workspace-scoped endpoint, e.g.
 * `/api/workspaces/${wsId}/cerebro/dictation/transcribe`.
 */
export function createHttpTranscriber(url: string, language?: string): Transcriber {
  return async (audio: Blob, signal: AbortSignal): Promise<string> => {
    const form = new FormData();
    // The filename extension is a hint; the backend decodes any container via
    // ffmpeg, so the exact value does not matter.
    form.append("file", audio, "dictation.webm");
    if (language) form.append("language", language);

    const res = await fetch(url, {
      method: "POST",
      body: form,
      credentials: "include",
      signal,
    });

    if (!res.ok) {
      throw new Error(`dictation transcribe failed: HTTP ${res.status}`);
    }

    // Defensive parse: never assume the body shape (see API Response
    // Compatibility). A missing/!string `text` degrades to an empty string
    // rather than throwing into the mic button.
    const data: unknown = await res.json().catch(() => null);
    const text =
      data && typeof data === "object" && typeof (data as { text?: unknown }).text === "string"
        ? (data as { text: string }).text
        : "";
    return text.trim();
  };
}
