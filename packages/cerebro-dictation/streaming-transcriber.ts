import type {
  DictationBackendErrorEvent,
  DictationFinalEvent,
  DictationPartialEvent,
  DictationStreamEvent,
  StreamingTranscriber,
  StreamingTranscriberOptions,
  StreamingTranscriberSession,
} from "./types";

export function createWebSocketStreamingTranscriber(
  url: string,
): StreamingTranscriber {
  return (options) => createSession(url, options);
}

function createSession(
  url: string,
  options: StreamingTranscriberOptions,
): StreamingTranscriberSession {
  const socket = new WebSocket(url);
  const queuedAudio: Blob[] = [];
  let settled = false;
  let receivedFinal = false;
  let receivedBackendError = false;
  let finishedResolve: (() => void) | undefined;
  let finishedReject: ((error: Error) => void) | undefined;

  const finished = new Promise<void>((resolve, reject) => {
    finishedResolve = resolve;
    finishedReject = reject;
  });

  const settleResolve = () => {
    if (settled) return;
    settled = true;
    finishedResolve?.();
  };
  const settleReject = (error: Error) => {
    if (settled) return;
    settled = true;
    finishedReject?.(error);
  };

  const sendJson = (payload: unknown) => {
    if (socket.readyState !== WebSocket.OPEN) return;
    socket.send(JSON.stringify(payload));
  };

  socket.addEventListener("open", () => {
    const token = readBrowserAuthToken();
    if (token) {
      sendJson({ type: "auth", payload: { token } });
    }
    sendJson({ type: "start", mime_type: options.mimeType });
    for (const audio of queuedAudio.splice(0)) {
      socket.send(audio);
    }
  });

  socket.addEventListener("message", (message) => {
    if (typeof message.data !== "string") return;
    const event = parseEvent(message.data);
    if (!event) return;
    if (event.type === "partial") {
      options.onPartial?.(event.text, event);
      return;
    }
    if (event.type === "final") {
      receivedFinal = true;
      options.onFinal?.(event.text, event);
      settleResolve();
      return;
    }
    receivedBackendError = true;
    const error = new Error(event.message);
    options.onError?.(error, event);
    settleReject(error);
  });

  socket.addEventListener("error", () => {
    const error = new Error("Dictation WebSocket failed.");
    options.onError?.(error);
    settleReject(error);
  });

  socket.addEventListener("close", () => {
    if (receivedFinal || receivedBackendError || settled) {
      settleResolve();
      return;
    }
    const error = new Error("Dictation stream closed before it returned a result.");
    options.onError?.(error);
    settleReject(error);
  });

  options.signal.addEventListener("abort", () => {
    sendJson({ type: "cancel" });
    socket.close();
    settleReject(new Error("Dictation was cancelled."));
  });

  return {
    sendAudio(audio: Blob) {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(audio);
        return;
      }
      if (socket.readyState === WebSocket.CONNECTING) {
        queuedAudio.push(audio);
      }
    },
    endUtterance() {
      sendJson({ type: "end_utterance" });
    },
    cancel() {
      sendJson({ type: "cancel" });
      socket.close();
    },
    finished,
  };
}

function readBrowserAuthToken(): string | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage.getItem("multica_token");
  } catch {
    return null;
  }
}

function parseEvent(raw: string): DictationStreamEvent | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const event = parsed as Partial<DictationStreamEvent>;
  if (event.type === "partial" && typeof event.text === "string") {
    return event as DictationPartialEvent;
  }
  if (event.type === "final" && typeof event.text === "string") {
    return event as DictationFinalEvent;
  }
  if (
    event.type === "error" &&
    typeof event.code === "string" &&
    typeof event.message === "string"
  ) {
    return event as DictationBackendErrorEvent;
  }
  return null;
}
