"use client";

import { useState } from "react";
import { Check, Copy, Loader2, Terminal } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { setupTokenOptions } from "@multica/core/runtimes/queries";
import { CODE_LIGATURE_CLASS } from "@multica/ui/lib/code-style";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const INSTALL_CMD =
  "curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash";

function normalizeURL(url: string | undefined): string {
  return url?.trim().replace(/\/+$/, "") ?? "";
}

interface Command {
  label: string;
  cmd: string;
}

/**
 * Build the connect command(s) for a given setup token.
 *
 *   - Cloud + token: a single install-and-connect one-liner. The installer's
 *     `--token` passthrough runs `multica setup --token` after installing, so
 *     one pasted line does everything (MUL-5112).
 *   - Self-host + token: two lines. The one-liner installer only knows the
 *     cloud endpoints, so the connect step must carry --server-url/--app-url
 *     itself via `multica setup self-host`.
 *   - No token (still minting, or mint failed): the classic browser flow, so
 *     the dialog degrades to a working command instead of a dead one.
 */
function buildCommands(
  token: string,
  installLabel: string,
  connectLabel: string,
  serverUrl?: string,
  appUrl?: string,
): Command[] {
  const server = normalizeURL(serverUrl);
  const app = normalizeURL(appUrl);
  const selfHost = server.length > 0 && app.length > 0;

  if (!token) {
    // Fallback: browser sign-in, no token embedded.
    const setup = selfHost
      ? `multica setup self-host --server-url ${server} --app-url ${app}`
      : "multica setup";
    return [
      { label: installLabel, cmd: INSTALL_CMD },
      { label: connectLabel, cmd: setup },
    ];
  }

  if (selfHost) {
    return [
      { label: installLabel, cmd: INSTALL_CMD },
      {
        label: connectLabel,
        cmd: `multica setup self-host --server-url ${server} --app-url ${app} --token ${token}`,
      },
    ];
  }

  // Cloud: one line installs the CLI and connects.
  return [
    { label: connectLabel, cmd: `${INSTALL_CMD} -s -- --token ${token}` },
  ];
}

function CopyButton({ text, ariaLabel }: { text: string; ariaLabel: string }) {
  const [copied, setCopied] = useState(false);
  const handleCopy = () => {
    void copyText(text).then((ok) => {
      if (!ok) return;
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };
  return (
    <button
      type="button"
      onClick={handleCopy}
      aria-label={ariaLabel}
      className="shrink-0 rounded p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      {copied ? (
        <Check className="h-3.5 w-3.5 text-success" aria-hidden />
      ) : (
        <Copy className="h-3.5 w-3.5" aria-hidden />
      )}
    </button>
  );
}

function CommandRow({ n, total, command }: { n: number; total: number; command: Command }) {
  return (
    <div>
      {/* Only number the step when there is more than one — a single cloud
          one-liner reads better unnumbered. */}
      <p className="mb-1.5 text-xs font-medium text-foreground">
        {total > 1 ? `${n}. ${command.label}` : command.label}
      </p>
      <div className="flex items-start gap-2 rounded-lg bg-muted px-3 py-2.5 font-mono text-sm">
        <Terminal className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <code
          className={cn(
            "min-w-0 flex-1 break-all whitespace-pre-wrap",
            CODE_LIGATURE_CLASS,
          )}
        >
          {command.cmd}
        </code>
        <CopyButton text={command.cmd} ariaLabel={command.label} />
      </div>
    </div>
  );
}

/**
 * Shared connect-command block for the "Connect from the terminal" /
 * "Add a computer" dialogs (MUL-5112). Mints a short-lived setup token while
 * the dialog is open and renders the one-command connect flow, so a headless
 * machine connects with a single paste — no browser round-trip. Falls back to
 * the browser-sign-in command if the mint is still in flight or fails, so the
 * dialog is never left without a working command.
 *
 * Pass serverUrl/appUrl for a self-hosted deployment; omit them for cloud.
 */
export function SetupCommandCard({
  wsId,
  open,
  serverUrl,
  appUrl,
}: {
  wsId: string;
  open: boolean;
  serverUrl?: string;
  appUrl?: string;
}) {
  const { t } = useT("runtimes");
  const tokenQuery = useQuery(setupTokenOptions(wsId, open));
  const token = tokenQuery.data?.token ?? "";

  const commands = buildCommands(
    token,
    t(($) => $.setup_command.install_label),
    t(($) => $.setup_command.connect_label),
    serverUrl,
    appUrl,
  );

  return (
    <div className="space-y-3">
      {commands.map((command, i) => (
        <CommandRow key={command.label} n={i + 1} total={commands.length} command={command} />
      ))}

      {tokenQuery.isPending && open ? (
        <p className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <Loader2 className="h-3 w-3 animate-spin" aria-hidden />
          {t(($) => $.setup_command.preparing)}
        </p>
      ) : token ? (
        <p className="text-[11px] leading-[1.5] text-muted-foreground">
          {t(($) => $.setup_command.expires_hint)}
        </p>
      ) : (
        // No token: the fallback command above uses browser sign-in.
        <p className="text-[11px] leading-[1.5] text-muted-foreground">
          {t(($) => $.setup_command.fallback_hint)}
        </p>
      )}
    </div>
  );
}
