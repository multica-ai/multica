import type { Transcriber } from "./types";

/**
 * Error thrown when the transcribe endpoint answers with a non-2xx status. It
 * carries the HTTP `status` and the backend `code` so the dictation hook can
 * tell a "warming up" cold start (503 `warming_up`, FIR-2048) apart from a hard
 * failure and react accordingly — keep the recording and offer a re-send rather
 * than discarding the clip.
 */
export class TranscribeError extends Error {
  readonly status: number;
  readonly code?: string;

  constructor(status: number, code?: string) {
    super(`dictation transcribe failed: HTTP ${status}`);
    this.name = "TranscribeError";
    this.status = status;
    this.code = code;
  }
}

/**
 * Fire-and-forget warmup: ping the dictation warmup endpoint the moment the
 * mic is pressed so the hviske engine cold-boots while the user is still
 * speaking (Jesper, FIR-1637). The engine scales to zero when idle, so without
 * this the first dictation after a pause pays the full ~30s cold start; with
 * it, the boot overlaps the recording and the real transcribe lands warm.
 *
 * Best-effort by design: the result is ignored and failures are swallowed —
 * the backend transcribe retry is the actual safety net, this is only the
 * latency optimisation.
 *
 * `url` is the workspace-scoped endpoint, e.g.
 * `/api/workspaces/${wsId}/cerebro/dictation/warmup`.
 */
export function createWarmup(url: string): () => void {
  return () => {
    void fetch(url, { method: "POST", credentials: "include" }).catch(() => {
      // Warmup is advisory — never surface its failure to the user.
    });
  };
}

/**
 * One-shot HTTP transcriber: record an utterance, POST it as multipart `file`
 * to the cerebro dictation transcribe endpoint, get the text back. This is the
 * non-streaming path used by the chat, notes, and document editors — the
 * backend proxies the clip to the cerebro-inference hviske model and returns
 * `{ text }`.
 *
 * `url` is the workspace-scoped endpoint, e.g.
 * `/api/workspaces/${wsId}/cerebro/dictation/transcribe`.
 *
 * `glossary` (comma-separated domain terms) biases decoding toward the user's
 * own words; `cleanup` opts into the LLM punctuation/structure pass. Both are
 * advisory — the backend ignores an empty glossary and only cleans up when its
 * own key is configured (FIR-1797).
 */
export function createHttpTranscriber(
  url: string,
  language?: string,
  glossary?: string,
  cleanup?: boolean,
): Transcriber {
  return async (audio: Blob, signal: AbortSignal): Promise<string> => {
    const form = new FormData();
    // The filename extension is a hint; the backend decodes any container via
    // ffmpeg, so the exact value does not matter.
    form.append("file", audio, "dictation.webm");
    if (language) form.append("language", language);
    if (glossary) form.append("glossary", glossary);
    if (cleanup) form.append("cleanup", "true");

    const res = await fetch(url, {
      method: "POST",
      body: form,
      credentials: "include",
      signal,
    });

    if (!res.ok) {
      // Read the backend error envelope ({ code, message }) when present so the
      // hook can distinguish a warming-up cold start from a real failure. The
      // body may be empty or non-JSON — degrade to status-only, never throw here.
      const body: unknown = await res.json().catch(() => null);
      const code =
        body && typeof body === "object" && typeof (body as { code?: unknown }).code === "string"
          ? (body as { code: string }).code
          : undefined;
      throw new TranscribeError(res.status, code);
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
