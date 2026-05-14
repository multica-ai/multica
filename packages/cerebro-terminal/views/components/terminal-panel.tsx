"use client";

import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { useTerminalSession } from "../use-terminal-session";

export interface TerminalPanelProps {
  runtimeId: string;
  command?: string[];
  /** Visual height in pixels — terminal fills horizontally. */
  height?: number;
  /** When false the panel stays mounted but the session is not opened. */
  enabled?: boolean;
}

/**
 * Drops an xterm.js view bound to a live cerebro terminal session. The
 * underlying child process runs server-side today; the same component
 * works unchanged when the broker is later swapped to a daemon-spawned
 * PTY (the wire protocol is identical).
 */
export function TerminalPanel({
  runtimeId,
  command,
  height = 360,
  enabled = true,
}: TerminalPanelProps) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);

  const sess = useTerminalSession({ runtimeId, command, enabled });

  // Mount xterm exactly once per host element.
  useEffect(() => {
    if (!hostRef.current) return;
    const term = new Terminal({
      convertEol: true,
      fontSize: 12,
      theme: { background: "#0b0d0e" },
      cursorBlink: true,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(hostRef.current);
    fit.fit();

    xtermRef.current = term;
    fitRef.current = fit;

    const onResize = () => fit.fit();
    window.addEventListener("resize", onResize);

    return () => {
      window.removeEventListener("resize", onResize);
      term.dispose();
      xtermRef.current = null;
      fitRef.current = null;
    };
  }, []);

  // Wire keyboard input → session stdin.
  useEffect(() => {
    const term = xtermRef.current;
    if (!term) return;
    const sub = term.onData((data) => sess.sendInput(data));
    return () => sub.dispose();
  }, [sess]);

  // Pipe session stdout → xterm.
  useEffect(() => {
    const term = xtermRef.current;
    if (!term) return;
    const off = sess.onOutput((bytes) => term.write(bytes));
    return off;
  }, [sess]);

  return (
    <div
      style={{
        height,
        background: "#0b0d0e",
        borderRadius: 6,
        overflow: "hidden",
        position: "relative",
      }}
    >
      <div
        ref={hostRef}
        style={{ width: "100%", height: "100%" }}
        aria-label="Interactive terminal"
      />
      {sess.status === "error" && (
        <div
          role="alert"
          style={{
            position: "absolute",
            inset: 0,
            display: "grid",
            placeItems: "center",
            background: "rgba(0,0,0,0.6)",
            color: "#fff",
            fontSize: 12,
          }}
        >
          Terminal error: {sess.error}
        </div>
      )}
    </div>
  );
}
