"use client";

import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { Button } from "@multica/ui/components/ui/button";
import { useTerminalSession } from "../use-terminal-session";

export interface TerminalPanelProps {
  runtimeId: string;
  /** Visual height in pixels; pass undefined to fill the parent. */
  height?: number | undefined;
  /** When false the panel stays mounted but the session is not opened. */
  enabled?: boolean;
}

/**
 * xterm.js view bound to a daemon-published terminal session for the runtime.
 *
 * The session is discovered via `GET /api/cerebro/terminal/runtimes/{id}/session`
 * (polled while no agent is attached) and streamed over a WebSocket. v1 is
 * read-only: keyboard input is dropped.
 */
export function TerminalPanel({
  runtimeId,
  height = 360,
  enabled = true,
}: TerminalPanelProps) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);

  const sess = useTerminalSession({ runtimeId, enabled });

  // Mount xterm exactly once per host element. xterm's FitAddon and
  // renderer both choke on a 0×0 host (the popout window can mount before
  // layout is final, manifesting as `Cannot read properties of undefined
  // (reading 'dimensions')` deep inside Viewport.syncScrollArea). Defer
  // `term.open` + the first `fit.fit()` until ResizeObserver reports
  // non-zero dimensions, then refit on every subsequent resize.
  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    const term = new Terminal({
      convertEol: true,
      fontSize: 12,
      theme: { background: "#0b0d0e" },
      cursorBlink: false,
      disableStdin: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    xtermRef.current = term;
    fitRef.current = fit;

    let opened = false;
    const tryOpenOrFit = () => {
      if (!host.isConnected || host.clientWidth === 0 || host.clientHeight === 0) return;
      if (!opened) {
        term.open(host);
        opened = true;
      }
      try {
        fit.fit();
      } catch {
        // FitAddon can throw if the renderer briefly loses its measurements
        // during a resize; the next observer tick will retry.
      }
    };

    const ro = new ResizeObserver(tryOpenOrFit);
    ro.observe(host);
    // Kick once in case the element is already sized on mount.
    tryOpenOrFit();

    return () => {
      ro.disconnect();
      term.dispose();
      xtermRef.current = null;
      fitRef.current = null;
    };
  }, []);

  // Pipe session stdout → xterm.
  useEffect(() => {
    const term = xtermRef.current;
    if (!term) return;
    const off = sess.onOutput((bytes) => term.write(bytes));
    return off;
  }, [sess]);

  const showXterm = sess.status === "connected" || sess.status === "closed";

  return (
    <div
      style={{
        height: height ?? "100%",
        width: "100%",
        background: "#0b0d0e",
        borderRadius: 6,
        overflow: "hidden",
        position: "relative",
      }}
    >
      <div
        ref={hostRef}
        style={{
          width: "100%",
          height: "100%",
          visibility: showXterm ? "visible" : "hidden",
        }}
        aria-label="Interactive terminal"
      />
      {sess.status === "waiting_for_agent" && (
        <div
          role="status"
          style={{
            position: "absolute",
            inset: 0,
            display: "grid",
            placeItems: "center",
            color: "#9ca3af",
            fontSize: 13,
          }}
        >
          <span style={{ opacity: 0.8 }} className="animate-pulse">
            Waiting for agent…
          </span>
        </div>
      )}
      {sess.status === "closed" && (
        <div
          role="status"
          style={{
            position: "absolute",
            left: 0,
            right: 0,
            bottom: 0,
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            padding: "6px 10px",
            background: "rgba(0,0,0,0.7)",
            color: "#e5e7eb",
            fontSize: 12,
          }}
        >
          <span>
            Agent session ended
            {sess.exitCode !== null ? ` (exit code ${sess.exitCode})` : ""}
            {sess.error ? ` — ${sess.error}` : ""}
          </span>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={sess.reattach}
          >
            Reattach
          </Button>
        </div>
      )}
      {sess.status === "error" && (
        <div
          role="alert"
          style={{
            position: "absolute",
            inset: 0,
            display: "grid",
            placeItems: "center",
            background: "rgba(0,0,0,0.6)",
            color: "#fca5a5",
            fontSize: 12,
            padding: 12,
            textAlign: "center",
          }}
        >
          Terminal error: {sess.error ?? "unknown"}
        </div>
      )}
    </div>
  );
}
