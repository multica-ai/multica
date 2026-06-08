import { useEffect, useRef, useState } from "react";
import { api } from "@multica/core/api";
import { type TerminalFrame, decodeStdout } from "../client";

export type TerminalSessionStatus =
  | "idle"
  | "waiting_for_agent"
  | "connected"
  | "closed"
  | "error";

export interface TerminalSessionHandle {
  status: TerminalSessionStatus;
  error: string | null;
  /** Most recent exit code received via an `exit` frame, if any. */
  exitCode: number | null;
  /** No-op in v1 (read-only display). Kept for symmetry with v2 stdin path. */
  sendInput: (input: string | Uint8Array) => void;
  /** Subscribe to decoded stdout chunks. Returns an unsubscribe function. */
  onOutput: (handler: (chunk: Uint8Array) => void) => () => void;
  /** Force a re-attempt: cancel any open WS/poll and start polling again. */
  reattach: () => void;
}

interface UseTerminalSessionArgs {
  runtimeId: string;
  /** When false the hook stays idle. Toggle to start lazily. */
  enabled?: boolean;
}

const POLL_INTERVAL_MS = 1500;

/**
 * Attaches to a daemon-published terminal session for the given runtime.
 *
 * Lifecycle:
 *   1. Poll `GET /api/cerebro/terminal/runtimes/{runtimeId}/session` until
 *      a session is published (status `waiting_for_agent`).
 *   2. Open a WebSocket at the returned `attach_path` (status `connected`).
 *   3. On WS close return to `waiting_for_agent` and resume polling — the
 *      daemon may reconnect and publish a fresh session.
 *
 * v1 is read-only: `sendInput` is a no-op. The WS frame shape is preserved
 * so v2 stdin wiring is a small change.
 */
export function useTerminalSession({
  runtimeId,
  enabled = true,
}: UseTerminalSessionArgs): TerminalSessionHandle {
  const [status, setStatus] = useState<TerminalSessionStatus>("idle");
  const [error, setError] = useState<string | null>(null);
  const [exitCode, setExitCode] = useState<number | null>(null);
  // Bump to force the lifecycle effect to re-run.
  const [reattachToken, setReattachToken] = useState(0);

  const wsRef = useRef<WebSocket | null>(null);
  const handlersRef = useRef<Set<(chunk: Uint8Array) => void>>(new Set());
  const stdinWarnedRef = useRef(false);

  useEffect(() => {
    if (!enabled || !runtimeId) {
      setStatus("idle");
      return;
    }

    let cancelled = false;
    let pollTimer: ReturnType<typeof setTimeout> | null = null;
    let ws: WebSocket | null = null;

    const clearPoll = () => {
      if (pollTimer !== null) {
        clearTimeout(pollTimer);
        pollTimer = null;
      }
    };

    const startPolling = () => {
      if (cancelled) return;
      setStatus("waiting_for_agent");
      setError(null);
      void poll();
    };

    const poll = async () => {
      if (cancelled) return;
      try {
        const info = await api.getActiveTerminalSession(runtimeId);
        if (cancelled) return;
        if (info) {
          openWs(info.attach_path);
          return;
        }
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
        // Don't surface as fatal — keep polling; the runtime may be
        // briefly unreachable. The status stays `waiting_for_agent`.
      }
      pollTimer = setTimeout(() => void poll(), POLL_INTERVAL_MS);
    };

    const openWs = (attachPath: string) => {
      if (cancelled) return;
      let socket: WebSocket;
      try {
        socket = new WebSocket(api.terminalAttachUrl(attachPath));
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
        setStatus("error");
        return;
      }
      ws = socket;
      wsRef.current = socket;

      // CEREBRO-PATCH(terminal-ws-auth): browsers cannot send Authorization
      // headers on WebSocket upgrades. If we have a bearer token (PAT users),
      // send it as the first message and wait for auth_ack before marking
      // "connected". Cookie-auth users have no stored token — the server reads
      // the multica_auth cookie directly and goes straight to the stream pump.
      const token = api.getToken();

      socket.onopen = () => {
        if (cancelled) return;
        setExitCode(null);
        setError(null);
        if (token) {
          socket.send(JSON.stringify({ type: "auth", payload: { token } }));
          // status transitions to "connected" on auth_ack — see onmessage below.
        } else {
          setStatus("connected");
        }
      };
      socket.onerror = () => {
        if (cancelled) return;
        setError("WebSocket error");
        // Don't transition to `error` here — `onclose` will fire next and
        // decide whether to resume polling or stay closed.
      };
      socket.onclose = () => {
        if (cancelled) return;
        wsRef.current = null;
        ws = null;
        // If we received an exit frame, status is already `closed`. Otherwise
        // the daemon may publish a fresh session — return to polling.
        setStatus((prev) => (prev === "closed" ? prev : "waiting_for_agent"));
        if (!cancelled) {
          clearPoll();
          pollTimer = setTimeout(() => void poll(), POLL_INTERVAL_MS);
        }
      };
      socket.onmessage = (ev) => {
        let frame: TerminalFrame;
        try {
          frame = JSON.parse(ev.data) as TerminalFrame;
        } catch {
          return;
        }
        if (frame.type === "auth_ack") {
          if (!cancelled) setStatus("connected");
          return;
        }
        if (frame.type === "auth_error") {
          if (!cancelled) {
            setError(frame.error ?? "auth failed");
            setStatus("error");
          }
          socket.close();
          return;
        }
        if (frame.type === "stdout" && typeof frame.data === "string") {
          const bytes = decodeStdout(frame.data);
          for (const h of handlersRef.current) h(bytes);
        } else if (frame.type === "exit") {
          setExitCode(typeof frame.code === "number" ? frame.code : null);
          if (frame.error) setError(frame.error);
          setStatus("closed");
        }
        // Unknown frame types are intentionally ignored (enum drift).
      };
    };

    startPolling();

    return () => {
      cancelled = true;
      clearPoll();
      if (ws && ws.readyState <= WebSocket.OPEN) ws.close();
      wsRef.current = null;
    };
  }, [runtimeId, enabled, reattachToken]);

  const sendInput = (_input: string | Uint8Array) => {
    // v1: read-only display. Wired in v2; see this hook's docstring.
    if (!stdinWarnedRef.current) {
      stdinWarnedRef.current = true;
      // eslint-disable-next-line no-console
      console.debug(
        "[cerebro-terminal] sendInput called but v1 is read-only — input dropped",
      );
    }
  };

  const onOutput = (handler: (chunk: Uint8Array) => void) => {
    handlersRef.current.add(handler);
    return () => {
      handlersRef.current.delete(handler);
    };
  };

  const reattach = () => {
    setError(null);
    setExitCode(null);
    setReattachToken((n) => n + 1);
  };

  return { status, error, exitCode, sendInput, onOutput, reattach };
}
