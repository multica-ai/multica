import { useEffect, useRef, useState } from "react";
import { api } from "@multica/core/api";
import {
  type TerminalFrame,
  type TerminalSessionInfo,
  decodeStdout,
  encodeStdin,
} from "../client";

export interface TerminalSessionHandle {
  status: "idle" | "opening" | "connected" | "closed" | "error";
  error: string | null;
  session: TerminalSessionInfo | null;
  /** Send a string (encoded UTF-8) or raw bytes to the child's stdin. */
  sendInput: (input: string | Uint8Array) => void;
  /** Subscribe to decoded stdout chunks. Returns an unsubscribe function. */
  onOutput: (handler: (chunk: Uint8Array) => void) => () => void;
  /** Tear down the session. */
  close: () => void;
}

interface UseTerminalSessionArgs {
  runtimeId: string;
  /** Optional argv; default 'sh' on server. */
  command?: string[];
  /** When false the hook stays idle. Toggle to open lazily. */
  enabled?: boolean;
}

/**
 * Opens an interactive terminal session for the given runtime. Lifecycle:
 *   1. POST /api/cerebro/terminal/sessions          (create)
 *   2. WebSocket /api/cerebro/terminal/sessions/{id}/ws  (attach)
 * The hook owns the WebSocket and tears it down on unmount or close().
 */
export function useTerminalSession({
  runtimeId,
  command,
  enabled = true,
}: UseTerminalSessionArgs): TerminalSessionHandle {
  const [status, setStatus] = useState<TerminalSessionHandle["status"]>("idle");
  const [error, setError] = useState<string | null>(null);
  const [session, setSession] = useState<TerminalSessionInfo | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const handlersRef = useRef<Set<(chunk: Uint8Array) => void>>(new Set());

  useEffect(() => {
    if (!enabled || !runtimeId) return;
    let cancelled = false;

    setStatus("opening");
    setError(null);

    (async () => {
      try {
        const info = await api.createTerminalSession(runtimeId, command);
        if (cancelled) {
          api.deleteTerminalSession(info.id).catch(() => {});
          return;
        }
        setSession(info);

        const ws = new WebSocket(api.terminalAttachUrl(info.attach_path));
        wsRef.current = ws;

        ws.onopen = () => setStatus("connected");
        ws.onerror = () => {
          setError("websocket error");
          setStatus("error");
        };
        ws.onclose = () => setStatus("closed");
        ws.onmessage = (ev) => {
          let frame: TerminalFrame;
          try {
            frame = JSON.parse(ev.data) as TerminalFrame;
          } catch {
            return;
          }
          if (frame.type === "stdout") {
            const bytes = decodeStdout(frame.data);
            for (const h of handlersRef.current) h(bytes);
          } else if (frame.type === "exit") {
            setStatus("closed");
            if (frame.error) setError(frame.error);
          }
        };
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
        setStatus("error");
      }
    })();

    return () => {
      cancelled = true;
      const ws = wsRef.current;
      if (ws && ws.readyState <= WebSocket.OPEN) {
        ws.close();
      }
      wsRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runtimeId, enabled]);

  const sendInput = (input: string | Uint8Array) => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify(encodeStdin(input)));
  };

  const onOutput = (handler: (chunk: Uint8Array) => void) => {
    handlersRef.current.add(handler);
    return () => {
      handlersRef.current.delete(handler);
    };
  };

  const close = () => {
    const s = session;
    setStatus("closed");
    wsRef.current?.close();
    wsRef.current = null;
    if (s) {
      api.deleteTerminalSession(s.id).catch(() => {});
    }
  };

  return { status, error, session, sendInput, onOutput, close };
}
