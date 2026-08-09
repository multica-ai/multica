"use client";

import { useEffect, useRef, useState } from "react";
import { AlertTriangle, Keyboard, Loader2, Radio, RotateCcw } from "lucide-react";
import { api } from "@multica/core/api";
import {
  TerminalClient,
  type TerminalConnectionState,
  type TerminalControlState,
  type TerminalServerMessage,
} from "@multica/core/terminal";
import type { TerminalSessionMetadata } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import "@xterm/xterm/css/xterm.css";

interface AgentTerminalProps {
  taskId: string;
  metadata: TerminalSessionMetadata;
}

export function AgentTerminal({ taskId, metadata }: AgentTerminalProps) {
  const { t } = useT("agents");
  const hostRef = useRef<HTMLDivElement>(null);
  const clientRef = useRef<TerminalClient | null>(null);
  const [connection, setConnection] = useState<TerminalConnectionState>("connecting");
  const [control, setControl] = useState<TerminalControlState>({ controller: false });
  const [notice, setNotice] = useState<string | null>(null);
  const [mobileReadOnly, setMobileReadOnly] = useState(false);
  const [processState, setProcessState] = useState(metadata.status ?? "reconnecting");
  const [structuredObservation, setStructuredObservation] = useState(
    metadata.structured_observation ?? "unavailable",
  );

  useEffect(() => {
    setStructuredObservation(metadata.structured_observation ?? "unavailable");
  }, [metadata.structured_observation]);

  useEffect(() => {
    setProcessState(metadata.status ?? "reconnecting");
  }, [metadata.status]);

  useEffect(() => {
    const query = window.matchMedia("(max-width: 767px)");
    const update = () => setMobileReadOnly(query.matches);
    update();
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    let disposed = false;
    let resizeObserver: ResizeObserver | null = null;
    let resizeFrame = 0;
    let terminalDispose: (() => void) | null = null;

    void Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")]).then(
      ([{ Terminal }, { FitAddon }]) => {
        if (disposed) return;
        const terminal = new Terminal({
          convertEol: false,
          cursorBlink: true,
          cursorStyle: "bar",
          fontFamily:
            'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace',
          fontSize: 13,
          scrollback: 10_000,
          theme: {
            background: "#0b1020",
            foreground: "#d8dee9",
            cursor: "#7dd3fc",
            selectionBackground: "#334155",
          },
        });
        const fitAddon = new FitAddon();
        terminal.loadAddon(fitAddon);
        terminal.open(host);

        const client = new TerminalClient(
          () => api.getTaskTerminalWebSocketConfig(taskId),
          {
            onOutput: (data) => terminal.write(data),
            onConnectionState: setConnection,
            onControl: setControl,
            onError: setNotice,
            onMessage: (message: TerminalServerMessage) => {
              if (message.structured_observation) {
                setStructuredObservation(message.structured_observation);
              }
              if (message.status) {
                setProcessState(message.status as typeof processState);
              }
              if (message.type === "gap") {
                setNotice(t(($) => $.cockpit.terminal_gap));
              } else if (message.type === "attached") {
                setNotice(null);
              } else if (message.type === "exit") {
                setProcessState("exited");
                setNotice(t(($) => $.cockpit.terminal_exited, { code: message.exit_code ?? "?" }));
              }
            },
          },
        );
        clientRef.current = client;
        const inputDisposable = terminal.onData((data) => client.sendInput(data));

        const fit = () => {
          if (disposed) return;
          fitAddon.fit();
          client.resize(terminal.cols, terminal.rows);
        };
        resizeObserver = new ResizeObserver(() => {
          cancelAnimationFrame(resizeFrame);
          resizeFrame = requestAnimationFrame(fit);
        });
        resizeObserver.observe(host);
        resizeFrame = requestAnimationFrame(fit);
        client.connect();

        terminalDispose = () => {
          inputDisposable.dispose();
          client.disconnect();
          terminal.dispose();
        };
      },
    );

    return () => {
      disposed = true;
      cancelAnimationFrame(resizeFrame);
      resizeObserver?.disconnect();
      terminalDispose?.();
      clientRef.current = null;
    };
  }, [taskId, t]);

  const connectionLabel = t(($) => $.cockpit[`terminal_${connection}`]);
  const observationLabel = t(
    ($) => $.cockpit[`structured_${structuredObservation}`],
  );

  return (
    <div className="flex h-full min-h-0 flex-col bg-[#0b1020]">
      <div className="flex flex-wrap items-center gap-2 border-b border-slate-700 bg-slate-950 px-3 py-2 text-caption text-slate-300">
        {connection === "connecting" || connection === "reconnecting" ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin text-sky-300" />
        ) : (
          <Radio
            className={cn(
              "h-3.5 w-3.5",
              connection === "connected" ? "text-emerald-400" : "text-slate-500",
            )}
          />
        )}
        <span>{connectionLabel}</span>
        <span className="text-slate-600">·</span>
        <span>{t(($) => $.cockpit[`process_${processState}`])}</span>
        {metadata.session_id && (
          <>
            <span className="text-slate-600">·</span>
            <span className="font-mono" title={metadata.session_id}>
              {metadata.session_id.slice(0, 8)}
            </span>
          </>
        )}
        <span className="text-slate-600">·</span>
        <span>
          {control.controller
            ? t(($) => $.cockpit.controller)
            : t(($) => $.cockpit.observer)}
        </span>
        <span className="text-slate-600">·</span>
        <span>{observationLabel}</span>
        {notice && (
          <span className="flex min-w-0 items-center gap-1 text-amber-300">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{notice}</span>
          </span>
        )}
        <div className="ml-auto flex items-center gap-1.5">
          {mobileReadOnly ? (
            <span className="rounded border border-slate-700 px-2 py-1 text-micro text-slate-400">
              {t(($) => $.cockpit.mobile_read_only)}
            </span>
          ) : control.controller ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="border-slate-600 bg-slate-900 text-slate-100 hover:bg-slate-800"
              onClick={() => clientRef.current?.releaseControl()}
            >
              <Keyboard className="h-3.5 w-3.5" />
              {t(($) => $.cockpit.release_control)}
            </Button>
          ) : (
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="border-slate-600 bg-slate-900 text-slate-100 hover:bg-slate-800"
              disabled={connection !== "connected"}
              onClick={() => clientRef.current?.claimControl()}
            >
              <Keyboard className="h-3.5 w-3.5" />
              {t(($) => $.cockpit.claim_control)}
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="border-slate-600 bg-slate-900 text-slate-100 hover:bg-slate-800"
            disabled={!control.controller || connection !== "connected"}
            onClick={() => clientRef.current?.ctrlC()}
          >
            <RotateCcw className="h-3.5 w-3.5" />
            {t(($) => $.cockpit.ctrl_c)}
          </Button>
        </div>
      </div>
      <div ref={hostRef} className="min-h-0 flex-1 overflow-hidden p-2" />
    </div>
  );
}
