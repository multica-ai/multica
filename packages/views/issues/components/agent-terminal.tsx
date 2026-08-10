"use client";

import { useEffect, useRef, useState } from "react";
import {
  AlertTriangle,
  ChevronDown,
  ChevronUp,
  Keyboard,
  Loader2,
  MoreHorizontal,
  Radio,
  Search,
  X,
} from "lucide-react";
import { api } from "@multica/core/api";
import {
  TerminalClient,
  type TerminalConnectionState,
  type TerminalControlState,
  type TerminalServerMessage,
} from "@multica/core/terminal";
import type { TerminalSessionMetadata } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { Terminal } from "@xterm/xterm";
import { useT } from "../../i18n";
import "@xterm/xterm/css/xterm.css";

interface AgentTerminalProps {
  taskId: string;
  metadata: TerminalSessionMetadata;
}

interface TerminalSearchController {
  findNext: (term: string, options?: { incremental?: boolean }) => boolean;
  findPrevious: (term: string) => boolean;
  clearDecorations: () => void;
}

const terminalTheme = {
  background: "#080c14",
  foreground: "#d7dde8",
  cursor: "#7dd3fc",
  cursorAccent: "#080c14",
  selectionBackground: "#334155cc",
  black: "#111827",
  red: "#f87171",
  green: "#86efac",
  yellow: "#fde68a",
  blue: "#93c5fd",
  magenta: "#d8b4fe",
  cyan: "#67e8f9",
  white: "#e5e7eb",
  brightBlack: "#64748b",
  brightRed: "#fca5a5",
  brightGreen: "#bbf7d0",
  brightYellow: "#fef3c7",
  brightBlue: "#bfdbfe",
  brightMagenta: "#e9d5ff",
  brightCyan: "#a5f3fc",
  brightWhite: "#f8fafc",
} as const;

export function AgentTerminal({ taskId, metadata }: AgentTerminalProps) {
  const { t } = useT("agents");
  const hostRef = useRef<HTMLDivElement>(null);
  const clientRef = useRef<TerminalClient | null>(null);
  const searchAddonRef = useRef<TerminalSearchController | null>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const [connection, setConnection] = useState<TerminalConnectionState>("connecting");
  const [control, setControl] = useState<TerminalControlState>({ controller: false });
  const [notice, setNotice] = useState<string | null>(null);
  const [mobileReadOnly, setMobileReadOnly] = useState(false);
  const [processState, setProcessState] = useState(metadata.status ?? "reconnecting");
  const [structuredObservation, setStructuredObservation] = useState(
    metadata.structured_observation ?? "unavailable",
  );
  const [searchOpen, setSearchOpen] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [hoveredLink, setHoveredLink] = useState<string | null>(null);

  useEffect(() => {
    if (!searchOpen) return;
    const frame = requestAnimationFrame(() => searchInputRef.current?.focus());
    return () => cancelAnimationFrame(frame);
  }, [searchOpen]);

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
    const terminal = new Terminal({
      // Unicode11Addon registers a width provider through xterm's proposed
      // Unicode API. This gate is intentionally scoped to this terminal
      // instance and is required by the official addon.
      allowProposedApi: true,
      convertEol: false,
      cursorBlink: true,
      cursorStyle: "block",
      cursorInactiveStyle: "outline",
      customGlyphs: true,
      fontFamily:
        '"SFMono-Regular", "Cascadia Code", "JetBrains Mono", Menlo, Monaco, Consolas, "Liberation Mono", monospace',
      fontSize: 14,
      lineHeight: 1.2,
      macOptionIsMeta: true,
      minimumContrastRatio: 4.5,
      rightClickSelectsWord: true,
      rescaleOverlappingGlyphs: true,
      screenReaderMode: true,
      scrollOnUserInput: true,
      scrollback: 10_000,
      smoothScrollDuration: 80,
      theme: terminalTheme,
    });
    const fitAddon = new FitAddon();
    const searchAddon = new SearchAddon();
    const unicodeAddon = new Unicode11Addon();
    const webLinksAddon = new WebLinksAddon(
      (_event, uri) => {
        try {
          const url = new URL(uri);
          if (url.protocol !== "http:" && url.protocol !== "https:") return;
          window.open(url.toString(), "_blank", "noopener,noreferrer");
        } catch {
          // Ignore malformed terminal-provided links.
        }
      },
      {
        hover: (_event, text) => setHoveredLink(text),
        leave: () => setHoveredLink(null),
      },
    );
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(searchAddon);
    terminal.loadAddon(unicodeAddon);
    terminal.unicode.activeVersion = "11";
    terminal.loadAddon(webLinksAddon);
    searchAddonRef.current = searchAddon;
    terminal.open(host);

    terminal.attachCustomKeyEventHandler((event) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "f") {
        if (event.type === "keydown") setSearchOpen(true);
        return false;
      }
      return true;
    });

    void import("@xterm/addon-webgl")
      .then(({ WebglAddon }) => {
        if (disposed) return;
        const webglAddon = new WebglAddon();
        webglAddon.onContextLoss(() => webglAddon.dispose());
        terminal.loadAddon(webglAddon);
      })
      .catch(() => {
        // The xterm DOM renderer remains active when WebGL2 is unavailable.
      });

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

    return () => {
      disposed = true;
      cancelAnimationFrame(resizeFrame);
      resizeObserver?.disconnect();
      inputDisposable.dispose();
      client.disconnect();
      terminal.dispose();
      clientRef.current = null;
      searchAddonRef.current = null;
    };
  }, [taskId, t]);

  const connectionLabel = t(($) => $.cockpit[`terminal_${connection}`]);
  const observationLabel = t(
    ($) => $.cockpit[`structured_${structuredObservation}`],
  );

  const closeSearch = () => {
    searchAddonRef.current?.clearDecorations();
    setSearchQuery("");
    setSearchOpen(false);
  };

  const updateSearch = (value: string) => {
    setSearchQuery(value);
    if (value) searchAddonRef.current?.findNext(value, { incremental: true });
    else searchAddonRef.current?.clearDecorations();
  };

  return (
    <div className="flex h-full min-h-0 flex-col bg-[#080c14]">
      <div className="flex min-h-11 shrink-0 items-center gap-3 border-b border-slate-800 bg-[#0d131f] px-3 text-caption text-slate-300">
        <div className="flex min-w-0 flex-1 items-center gap-2 overflow-hidden whitespace-nowrap">
          {connection === "connecting" || connection === "reconnecting" ? (
            <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-sky-300" />
          ) : (
            <Radio
              className={cn(
                "h-3.5 w-3.5 shrink-0",
                connection === "connected" ? "text-emerald-400" : "text-slate-500",
              )}
            />
          )}
          <span className="font-medium text-slate-100">{connectionLabel}</span>
          <span
            className={cn(
              "rounded-full border px-2 py-0.5 text-micro",
              processState === "running"
                ? "border-emerald-500/25 bg-emerald-500/10 text-emerald-300"
                : "border-slate-700 bg-slate-800/70 text-slate-300",
            )}
          >
            {t(($) => $.cockpit[`process_${processState}`])}
          </span>
          {metadata.session_id && (
            <span
              className="hidden rounded border border-slate-800 bg-slate-950/70 px-1.5 py-0.5 font-mono text-micro text-slate-400 sm:inline"
              title={metadata.session_id}
            >
              {metadata.session_id.slice(0, 8)}
            </span>
          )}
          <span className="hidden truncate text-slate-500 lg:inline">{observationLabel}</span>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="text-slate-400 hover:bg-slate-800 hover:text-slate-100"
            aria-label={t(($) => $.cockpit.search_terminal)}
            onClick={() => setSearchOpen(true)}
          >
            <Search className="h-3.5 w-3.5" />
          </Button>
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
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  className="text-slate-400 hover:bg-slate-800 hover:text-slate-100"
                  aria-label={t(($) => $.cockpit.terminal_actions)}
                />
              }
            >
              <MoreHorizontal className="h-4 w-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                disabled={!control.controller || connection !== "connected"}
                onClick={() => clientRef.current?.ctrlC()}
              >
                {t(($) => $.cockpit.send_ctrl_c)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
      {searchOpen && (
        <div className="flex shrink-0 items-center gap-1.5 border-b border-slate-800 bg-slate-950 px-3 py-1.5">
          <Search className="h-3.5 w-3.5 shrink-0 text-slate-500" />
          <input
            ref={searchInputRef}
            value={searchQuery}
            onChange={(event) => updateSearch(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                if (event.shiftKey) searchAddonRef.current?.findPrevious(searchQuery);
                else searchAddonRef.current?.findNext(searchQuery);
              } else if (event.key === "Escape") {
                closeSearch();
              }
            }}
            placeholder={t(($) => $.cockpit.search_placeholder)}
            aria-label={t(($) => $.cockpit.search_terminal)}
            className="h-7 min-w-0 flex-1 bg-transparent font-mono text-caption text-slate-100 outline-none placeholder:text-slate-600"
          />
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="text-slate-400 hover:bg-slate-800 hover:text-slate-100"
            aria-label={t(($) => $.cockpit.previous_match)}
            disabled={!searchQuery}
            onClick={() => searchAddonRef.current?.findPrevious(searchQuery)}
          >
            <ChevronUp className="h-3.5 w-3.5" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="text-slate-400 hover:bg-slate-800 hover:text-slate-100"
            aria-label={t(($) => $.cockpit.next_match)}
            disabled={!searchQuery}
            onClick={() => searchAddonRef.current?.findNext(searchQuery)}
          >
            <ChevronDown className="h-3.5 w-3.5" />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="text-slate-400 hover:bg-slate-800 hover:text-slate-100"
            aria-label={t(($) => $.cockpit.close_search)}
            onClick={closeSearch}
          >
            <X className="h-3.5 w-3.5" />
          </Button>
        </div>
      )}
      {notice && (
        <div className="flex shrink-0 items-center gap-1.5 border-b border-amber-400/15 bg-amber-400/5 px-3 py-1.5 text-micro text-amber-300">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate">{notice}</span>
        </div>
      )}
      <div className="relative min-h-0 flex-1 overflow-hidden">
        <div ref={hostRef} className="h-full cursor-text overflow-hidden px-3 py-2.5" />
        {hoveredLink && (
          <div className="pointer-events-none absolute bottom-2 left-3 max-w-[calc(100%-1.5rem)] truncate rounded border border-slate-700 bg-slate-950/95 px-2 py-1 font-mono text-micro text-slate-300 shadow-lg">
            {hoveredLink}
          </div>
        )}
      </div>
    </div>
  );
}
