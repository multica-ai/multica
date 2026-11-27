"use client";

import { useState, type ReactNode } from "react";
import {
  AlertCircle,
  Check,
  CircleCheck,
  CircleDashed,
  Copy,
  Download,
  Loader2,
  LogIn,
  RotateCcw,
  Terminal,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import type { CodingToolPhase } from "@multica/core/onboarding";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { openExternal } from "../../platform";
import { useT } from "../../i18n";
import type { CodingToolInfo } from "./coding-tool-catalog";
import type { CodingToolRun } from "./use-coding-tools";

export type CodingToolRowState =
  | "checking"
  | "ready"
  | "connecting"
  | "needs_login"
  | "installing"
  | "failed"
  | "missing";

/**
 * One tool in the step-3 list. A row is selectable only when ready; while a
 * run is going it expands to show the phases, and a failed run expands into
 * the three ways forward (retry, another tool, the terminal).
 */
export function CodingToolRow({
  tool,
  state,
  version,
  fresh,
  tested,
  anyReady,
  selected,
  busy,
  run,
  switchTo,
  switchKind = "install",
  showTerminal,
  onSelect,
  onInstall,
  onLogin,
  onCancel,
  onRetry,
  onSwitch,
  onShowTerminal,
  onSubmitCode,
}: {
  tool: CodingToolInfo;
  state: CodingToolRowState;
  version: string | null;
  /** Installed or signed in during this visit. */
  fresh: boolean;
  /** A test prompt passed during this visit. */
  tested: boolean;
  anyReady: boolean;
  selected: boolean;
  /** Another tool is installing; one run at a time keeps the screen legible. */
  busy: boolean;
  run?: CodingToolRun;
  /** The tool the failure options offer instead. */
  switchTo?: CodingToolInfo;
  /** Whether switching installs the other tool or only signs in to it. */
  switchKind?: "install" | "login";
  showTerminal: boolean;
  onSelect: () => void;
  onInstall: () => void;
  onLogin: () => void;
  onCancel: () => void;
  onRetry: () => void;
  onSwitch?: () => void;
  onShowTerminal: () => void;
  onSubmitCode: (code: string) => Promise<boolean>;
}) {
  const { t } = useT("onboarding");
  const selectable = state === "ready";
  const versionPrefix = version ? `${version} · ` : "";

  let description: ReactNode;
  let trailing: ReactNode = null;
  switch (state) {
    case "checking":
      description = t(($) => $.step_agent.status_checking);
      trailing = <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />;
      break;
    case "ready":
      description = versionPrefix + (tested ? t(($) => $.step_agent.status_ready_tested) : t(($) => $.step_agent.status_ready));
      trailing = (
        <>
          <span className="inline-flex items-center gap-1 rounded-full bg-success/10 px-2 py-0.5 text-caption font-medium text-success">
            <Check className="h-3 w-3" />
            {t(($) => $.step_agent.badge_ready)}
          </span>
          <span
            aria-hidden
            className={cn(
              "flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-[1.5px]",
              selected ? "border-foreground" : "border-muted-foreground/40",
            )}
          >
            {selected && <span className="h-2 w-2 rounded-full bg-foreground" />}
          </span>
        </>
      );
      break;
    case "connecting":
      description = versionPrefix + t(($) => $.step_agent.status_connecting);
      trailing = <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />;
      break;
    case "needs_login":
      description = versionPrefix + t(($) => $.step_agent.status_needs_login);
      trailing = (
        <Button size="sm" variant="outline" disabled={busy} onClick={onLogin}>
          <LogIn className="h-3.5 w-3.5" />
          {t(($) => $.step_agent.action_login)}
        </Button>
      );
      break;
    case "installing":
      description =
        run?.kind === "login"
          ? t(($) => $.step_agent.status_logging_in)
          : t(($) => $.step_agent.status_installing, { count: tool.minutes });
      trailing = (
        <Button size="sm" variant="ghost" disabled={!run?.runId} onClick={onCancel}>
          {t(($) => $.common.cancel)}
        </Button>
      );
      break;
    case "failed":
      description =
        run?.error?.code === "not_detected"
          ? versionPrefix + t(($) => $.step_agent.status_not_detected)
          : t(($) => $.step_agent.status_failed);
      break;
    case "missing":
      description = anyReady
        ? t(($) => $.step_agent.status_missing)
        : t(($) => $.step_agent.vendor[tool.id]);
      trailing = (
        <Button size="sm" variant="outline" disabled={busy} onClick={onInstall}>
          <Download className="h-3.5 w-3.5" />
          {t(($) => $.step_agent.action_install)}
        </Button>
      );
      break;
  }

  return (
    <div
      className={cn(
        "rounded-lg border bg-card transition-colors",
        selectable && "cursor-pointer hover:border-foreground/30",
        selected && selectable && "border-foreground",
        state === "failed" && "border-destructive/40",
      )}
      onClick={selectable ? onSelect : undefined}
      role={selectable ? "radio" : undefined}
      aria-checked={selectable ? selected : undefined}
      tabIndex={selectable ? 0 : undefined}
      onKeyDown={
        selectable
          ? (event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                onSelect();
              }
            }
          : undefined
      }
      data-testid={`coding-tool-${tool.id}`}
      data-state={state}
    >
      <div className="flex items-center gap-3 px-4 py-3">
        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border bg-background">
          <ProviderLogo provider={tool.provider} className="h-4 w-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 text-body font-medium text-foreground">
            {tool.name}
            {fresh && state === "ready" && (
              <span className="rounded-full bg-brand/10 px-2 py-0.5 text-caption font-medium text-brand">
                {t(($) => $.step_agent.badge_new)}
              </span>
            )}
          </div>
          <div className="truncate text-caption text-muted-foreground">{description}</div>
        </div>
        <div className="flex shrink-0 items-center gap-2">{trailing}</div>
      </div>

      {state === "installing" && run && (
        <RunProgress tool={tool} run={run} onSubmitCode={onSubmitCode} />
      )}

      {state === "failed" && run && (
        <FailureOptions
          tool={tool}
          run={run}
          switchTo={switchTo}
          switchKind={switchKind}
          showTerminal={showTerminal}
          onRetry={onRetry}
          onSwitch={onSwitch}
          onShowTerminal={onShowTerminal}
        />
      )}
    </div>
  );
}

const INSTALL_PHASES: CodingToolPhase[] = ["download", "install", "login", "verify"];
const LOGIN_PHASES: CodingToolPhase[] = ["login", "verify"];

function RunProgress({
  tool,
  run,
  onSubmitCode,
}: {
  tool: CodingToolInfo;
  run: CodingToolRun;
  onSubmitCode: (code: string) => Promise<boolean>;
}) {
  const { t } = useT("onboarding");
  const phases = run.kind === "install" ? INSTALL_PHASES : LOGIN_PHASES;
  const current = phases.indexOf(run.phase);

  const label = (phase: CodingToolPhase, done: boolean) => {
    switch (phase) {
      case "download":
        return done ? t(($) => $.step_agent.phase_download_done) : t(($) => $.step_agent.phase_download);
      case "install":
        return done ? t(($) => $.step_agent.phase_install_done) : t(($) => $.step_agent.phase_install);
      case "login":
        return done
          ? t(($) => $.step_agent.phase_login_done, { account: tool.account })
          : t(($) => $.step_agent.phase_login, { account: tool.account });
      case "verify":
        return done ? t(($) => $.step_agent.phase_verify_done) : t(($) => $.step_agent.phase_verify);
    }
  };

  return (
    <ol className="flex flex-col gap-2.5 border-t px-4 py-3" aria-live="polite">
      {phases.map((phase, index) => {
        const done = index < current;
        const active = index === current;
        return (
          <li key={phase} className="flex items-start gap-2.5 text-caption">
            {done ? (
              <CircleCheck className="mt-px h-4 w-4 shrink-0 text-success" />
            ) : active ? (
              <Loader2 className="mt-px h-4 w-4 shrink-0 animate-spin text-foreground" />
            ) : (
              <CircleDashed className="mt-px h-4 w-4 shrink-0 text-faint-foreground" />
            )}
            <div className={cn("min-w-0 flex-1", done || active ? "text-foreground" : "text-muted-foreground")}>
              {label(phase, done)}
              {active && phase === "download" && run.progress !== undefined && (
                <div className="mt-1.5 h-1 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-foreground transition-[width]"
                    style={{ width: `${Math.round(run.progress * 100)}%` }}
                  />
                </div>
              )}
              {active && phase === "login" && <LoginWaiting tool={tool} run={run} onSubmitCode={onSubmitCode} />}
            </div>
          </li>
        );
      })}
    </ol>
  );
}

function LoginWaiting({
  tool,
  run,
  onSubmitCode,
}: {
  tool: CodingToolInfo;
  run: CodingToolRun;
  onSubmitCode: (code: string) => Promise<boolean>;
}) {
  const { t } = useT("onboarding");
  const [code, setCode] = useState("");
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState(false);
  const [failed, setFailed] = useState(false);

  const submit = async () => {
    if (!code.trim() || sending) return;
    setSending(true);
    setFailed(false);
    const ok = await onSubmitCode(code);
    setSending(false);
    if (ok) setSent(true);
    else setFailed(true);
  };

  return (
    <div className="mt-1 flex flex-col gap-2 text-muted-foreground">
      <p>
        {t(($) => $.step_agent.login_waiting, { account: tool.account })}
        {run.loginUrl && (
          <>
            {" "}
            <button
              type="button"
              className="font-medium text-foreground underline-offset-2 hover:underline"
              onClick={() => openExternal(run.loginUrl!)}
            >
              {t(($) => $.step_agent.login_reopen)}
            </button>
          </>
        )}
      </p>
      {run.needsCode && !sent && (
        <form
          className="flex flex-col gap-1.5"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <span>{t(($) => $.step_agent.login_code_prompt)}</span>
          <div className="flex gap-2">
            <Input
              value={code}
              onChange={(event) => setCode(event.target.value)}
              placeholder={t(($) => $.step_agent.login_code_placeholder)}
              className="h-8 font-mono text-caption"
              autoComplete="off"
              spellCheck={false}
            />
            <Button type="submit" size="sm" disabled={!code.trim() || sending}>
              {sending && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {t(($) => $.step_agent.login_code_submit)}
            </Button>
          </div>
          {failed && (
            <span className="text-destructive">
              {t(($) => $.step_agent.login_code_failed, { tool: tool.name })}
            </span>
          )}
        </form>
      )}
    </div>
  );
}

function FailureOptions({
  tool,
  run,
  switchTo,
  switchKind = "install",
  showTerminal,
  onRetry,
  onSwitch,
  onShowTerminal,
}: {
  tool: CodingToolInfo;
  run: CodingToolRun;
  switchTo?: CodingToolInfo;
  /** Whether switching installs the other tool or only signs in to it. */
  switchKind?: "install" | "login";
  showTerminal: boolean;
  onRetry: () => void;
  onSwitch?: () => void;
  onShowTerminal: () => void;
}) {
  const { t } = useT("onboarding");
  const [logOpen, setLogOpen] = useState(false);
  const code = run.error?.code ?? "unknown";
  const title =
    code === "verify_failed" || code === "not_detected"
      ? t(($) => $.step_agent.error[code], { tool: tool.name })
      : code === "network" || code === "permission" || code === "unsupported_platform" || code === "login_failed"
        ? t(($) => $.step_agent.error[code])
        : t(($) => $.step_agent.error.unknown);
  // Installed but not signed in, or signed in but the test failed: the
  // terminal route is signing in again, not installing again.
  const command = run.phase === "login" || run.phase === "verify" ? tool.loginCommand : tool.terminalCommand;

  return (
    <div className="flex flex-col gap-3 border-t px-4 py-3">
      <div className="flex items-start gap-2.5 rounded-md bg-destructive/5 px-3 py-2.5 text-caption">
        <AlertCircle className="mt-px h-4 w-4 shrink-0 text-destructive" />
        <div className="min-w-0 flex-1">
          <div className="font-medium text-foreground">{title}</div>
          {run.error?.message && (
            <div className="mt-0.5 break-words font-mono text-muted-foreground">{run.error.message}</div>
          )}
          {logOpen && run.logTail && (
            <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap rounded-sm bg-muted p-2 font-mono text-[11px] leading-relaxed text-muted-foreground">
              {run.logTail}
            </pre>
          )}
        </div>
        {run.logTail && (
          <button
            type="button"
            className="shrink-0 text-muted-foreground hover:text-foreground"
            onClick={() => setLogOpen((open) => !open)}
          >
            {logOpen ? t(($) => $.step_agent.hide_log) : t(($) => $.step_agent.view_log)}
          </button>
        )}
      </div>

      <FailureOption
        icon={<RotateCcw className="h-4 w-4" />}
        title={t(($) => $.step_agent.fail_retry_title)}
        description={t(($) => $.step_agent.fail_retry_desc)}
        action={
          <Button size="sm" variant="outline" onClick={onRetry}>
            {t(($) => $.step_agent.action_retry)}
          </Button>
        }
      />
      {switchTo && onSwitch && (
        <FailureOption
          icon={<ProviderLogo provider={switchTo.provider} className="h-4 w-4" />}
          title={t(($) => $.step_agent.fail_switch_title, { tool: switchTo.name })}
          description={t(($) => $.step_agent.fail_switch_desc)}
          action={
            <Button size="sm" variant="outline" onClick={onSwitch}>
              {switchKind === "login" ? (
                <LogIn className="h-3.5 w-3.5" />
              ) : (
                <Download className="h-3.5 w-3.5" />
              )}
              {switchKind === "login"
                ? t(($) => $.step_agent.action_login)
                : t(($) => $.step_agent.action_install)}
            </Button>
          }
        />
      )}
      <FailureOption
        icon={<Terminal className="h-4 w-4" />}
        title={t(($) => $.step_agent.fail_terminal_title)}
        description={
          showTerminal ? (
            <TerminalCommand command={command} />
          ) : (
            t(($) => $.step_agent.fail_terminal_desc)
          )
        }
        action={
          showTerminal ? null : (
            <Button size="sm" variant="outline" onClick={onShowTerminal}>
              {t(($) => $.step_agent.show_command)}
            </Button>
          )
        }
      />
    </div>
  );
}

function FailureOption({
  icon,
  title,
  description,
  action,
}: {
  icon: ReactNode;
  title: ReactNode;
  description: ReactNode;
  action: ReactNode;
}) {
  return (
    <div className="flex items-start gap-3">
      <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center text-muted-foreground">{icon}</span>
      <div className="min-w-0 flex-1">
        <div className="text-caption font-medium text-foreground">{title}</div>
        <div className="text-caption text-muted-foreground">{description}</div>
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}

function TerminalCommand({ command }: { command: string }) {
  const { t } = useT("onboarding");
  const [copied, setCopied] = useState(false);
  return (
    <div className="mt-1 flex flex-col gap-1.5">
      <div className="flex items-center gap-2 rounded-md bg-muted px-2.5 py-1.5">
        <code className="min-w-0 flex-1 break-all font-mono text-[11px] text-foreground">{command}</code>
        <button
          type="button"
          aria-label={t(($) => $.step_agent.copy)}
          className="shrink-0 text-muted-foreground hover:text-foreground"
          onClick={async () => {
            if (await copyText(command)) {
              setCopied(true);
              window.setTimeout(() => setCopied(false), 1500);
            }
          }}
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      </div>
      <span className="flex items-center gap-1.5">
        <span aria-hidden className="inline-block size-1.5 rounded-full bg-success animate-pulse" />
        {t(($) => $.step_agent.terminal_waiting)}
      </span>
    </div>
  );
}
