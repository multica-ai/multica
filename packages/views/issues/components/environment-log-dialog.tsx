"use client";

import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { issueEnvironmentCommandOptions } from "@multica/core/issues/environments";
import type { IssueEnvironment } from "@multica/core/types";
import { ArrowDownToLine, Copy, RefreshCw, RotateCw, Search, Server, WrapText } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { Toggle } from "@multica/ui/components/ui/toggle";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT } from "../../i18n";
import { classifyLogLines, environmentTitle, exitCode, isLive } from "./environment-model";

const FOLLOW_INTERVAL = 5_000;

const LEVEL_CLASS = {
  error: "bg-destructive/10 text-destructive",
  warn: "text-warning",
} as const;

interface LogSnapshot {
  output: string;
  readAt: string;
}

/**
 * The end of a declared environment's log, read from its machine on demand:
 * the machine answers a log command with at most 500 lines (256 KB), and the
 * dialog keeps re-asking while the environment runs and "follow" is on. An
 * offline machine cannot answer, so the dialog falls back to the lines the
 * environment printed before it crashed, when there are any.
 */
export function EnvironmentLogDialog({
  issueId,
  env,
  open,
  onOpenChange,
  machineTitle,
  originLabel,
  offline,
  canRestart,
  onRestart,
}: {
  issueId: string;
  env: IssueEnvironment;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  machineTitle: string | null;
  originLabel: string | null;
  offline: boolean;
  canRestart: boolean;
  onRestart: () => void;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const [commandId, setCommandId] = useState<string | null>(null);
  const [snapshot, setSnapshot] = useState<LogSnapshot | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [wrap, setWrap] = useState(true);
  const [follow, setFollow] = useState(true);
  const bodyRef = useRef<HTMLDivElement>(null);
  const live = isLive(env);

  const request = useMutation({
    mutationFn: () => api.readIssueEnvironmentLogs(issueId, env.id),
    onSuccess: (command) => {
      setFailed(null);
      setCommandId(command.id || null);
    },
    onError: (error) => setFailed(error instanceof Error ? error.message : String(error)),
  });
  const { data: command } = useQuery(issueEnvironmentCommandOptions(issueId, commandId));
  const reading = request.isPending || command?.status === "pending";

  useEffect(() => {
    if (!command || command.id !== commandId) return;
    if (command.status === "succeeded") {
      setSnapshot({ output: command.output ?? "", readAt: command.completed_at ?? new Date().toISOString() });
      setFailed(null);
    } else if (command.status === "failed" || command.status === "expired") {
      setFailed(command.error || t(($) => $.environments.log.no_response));
    }
  }, [command, commandId, t]);

  const refresh = request.mutate;
  useEffect(() => {
    if (open && !offline) refresh();
  }, [open, offline, refresh]);

  useEffect(() => {
    if (!open || offline || !live || !follow || reading || failed) return;
    const timer = setTimeout(() => refresh(), FOLLOW_INTERVAL);
    return () => clearTimeout(timer);
  }, [open, offline, live, follow, reading, failed, refresh, snapshot]);

  const lines = useMemo(() => {
    const text = offline ? env.excerpt : snapshot?.output ?? "";
    return text ? text.replace(/\n$/, "").split("\n") : [];
  }, [offline, env.excerpt, snapshot]);
  const levels = useMemo(() => classifyLogLines(lines), [lines]);
  const visible = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    return lines
      .map((text, index) => ({ text, index, level: levels[index] ?? null }))
      .filter((line) => !needle || line.text.toLowerCase().includes(needle));
  }, [lines, levels, filter]);

  useLayoutEffect(() => {
    if (!follow || !bodyRef.current) return;
    bodyRef.current.scrollTop = bodyRef.current.scrollHeight;
  }, [follow, visible]);

  const timeOf = (iso: string, seconds: boolean) =>
    new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", ...(seconds ? { second: "2-digit" } : {}), hour12: false }).format(new Date(iso));

  const code = exitCode(env);
  const meta = [
    code !== null && code !== 0 ? t(($) => $.environments.exit_code, { code }) : null,
    machineTitle,
    originLabel,
  ].filter(Boolean).join(" · ");

  let divider: string | null = null;
  if (env.ended_at && !filter) {
    const at = timeOf(env.ended_at, true);
    if (env.state === "stopped") divider = t(($) => $.environments.log.stopped_divider, { time: at });
    else if (code !== null && code !== 0) divider = t(($) => $.environments.log.exit_divider_code, { time: at, code });
    else divider = t(($) => $.environments.log.exit_divider, { time: at });
  }

  const copyLog = () => {
    const text = visible.map((line) => line.text).join("\n");
    void copyText(text).then((ok) => {
      if (ok) toast.success(t(($) => $.environments.log.copied));
      else toast.error(t(($) => $.environments.log.copy_failed));
    });
  };

  let empty: string | null = null;
  if (lines.length === 0) {
    if (offline) empty = t(($) => $.environments.log.offline_empty);
    else if (failed) empty = null;
    else if (!snapshot) empty = t(($) => $.environments.log.loading);
    else empty = t(($) => $.environments.log.empty);
  } else if (visible.length === 0) {
    empty = t(($) => $.environments.log.no_match);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex h-[min(640px,calc(100%-4rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-[880px]">
        <div className="border-b px-4 pt-4 pb-3 pr-12">
          <DialogTitle className="flex min-w-0 items-center gap-2 text-body font-medium">
            <Server className={cn("size-4 shrink-0", env.state === "exited" ? "text-destructive" : "text-muted-foreground")} />
            <span className="truncate">{t(($) => $.environments.log.title, { name: environmentTitle(env) })}</span>
          </DialogTitle>
          <DialogDescription className="mt-1 flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
            <span className={cn("shrink-0", env.state === "exited" ? "text-destructive" : env.state === "running" ? "text-success" : undefined)}>
              {t(($) => $.environments.state[env.state])}
            </span>
            {meta && <span aria-hidden="true">·</span>}
            {meta && <span className="min-w-0 truncate">{meta}</span>}
          </DialogDescription>
        </div>

        <div className="flex items-center gap-2 border-b px-4 py-2">
          <div className="relative w-56">
            <Search aria-hidden="true" className="pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              placeholder={t(($) => $.environments.log.filter)}
              aria-label={t(($) => $.environments.log.filter)}
              className="h-7 pl-7 text-caption"
            />
          </div>
          <Toggle size="sm" pressed={wrap} onPressedChange={setWrap} aria-label={t(($) => $.environments.log.wrap)}>
            <WrapText className="size-3.5" />
            {t(($) => $.environments.log.wrap)}
          </Toggle>
          {live && !offline && (
            <Toggle size="sm" pressed={follow} onPressedChange={setFollow} aria-label={t(($) => $.environments.log.follow)}>
              <ArrowDownToLine className="size-3.5" />
              {t(($) => $.environments.log.follow)}
            </Toggle>
          )}
          <div className="flex-1" />
          <Button variant="ghost" size="sm" onClick={copyLog} disabled={visible.length === 0}>
            <Copy className="size-3.5" />
            {t(($) => $.environments.log.copy)}
          </Button>
        </div>

        {offline && (
          <p className="border-b bg-muted/50 px-4 py-2 text-caption text-muted-foreground">
            {env.excerpt ? t(($) => $.environments.log.offline_excerpt) : t(($) => $.environments.offline_hint)}
          </p>
        )}
        {failed && !offline && (
          <p role="alert" className="border-b bg-destructive/10 px-4 py-2 text-caption text-destructive">
            {t(($) => $.environments.log.failed, { error: failed })}
          </p>
        )}

        <div
          ref={bodyRef}
          data-testid="environment-log"
          onWheel={(event) => {
            if (event.deltaY < 0 && follow) setFollow(false);
          }}
          className="min-h-0 flex-1 overflow-auto py-2 font-mono text-caption leading-5"
        >
          {visible.map((line) => (
            <div key={line.index} data-level={line.level ?? undefined} className={cn("flex px-4", line.level && LEVEL_CLASS[line.level])}>
              <span aria-hidden="true" className="w-10 shrink-0 pr-3 text-right text-faint-foreground tabular-nums select-none">{line.index + 1}</span>
              <span className={cn("min-w-0 flex-1", wrap ? "whitespace-pre-wrap break-all" : "whitespace-pre")}>{line.text || " "}</span>
            </div>
          ))}
          {empty && <p className="px-4 py-6 text-center text-caption text-muted-foreground">{empty}</p>}
          {divider && lines.length > 0 && (
            <div className="mx-4 my-2 flex items-center gap-3 text-micro text-muted-foreground">
              <span className="h-px flex-1 bg-border" />
              <span>{divider}</span>
              <span className="h-px flex-1 bg-border" />
            </div>
          )}
        </div>

        <div className="flex items-center gap-2 border-t px-4 py-3">
          <p className="min-w-0 flex-1 truncate text-caption text-muted-foreground tabular-nums">
            {snapshot && !offline
              ? t(($) => $.environments.log.footer, { count: lines.length, time: timeOf(snapshot.readAt, false) })
              : null}
          </p>
          {!offline && (
            <Button variant="outline" size="sm" onClick={() => refresh()} disabled={reading}>
              {reading ? <Spinner className="size-3.5" /> : <RefreshCw className="size-3.5" />}
              {t(($) => $.environments.log.refresh)}
            </Button>
          )}
          {canRestart && (
            <Button size="sm" onClick={onRestart}>
              <RotateCw className="size-3.5" />
              {t(($) => $.environments.restart)}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
