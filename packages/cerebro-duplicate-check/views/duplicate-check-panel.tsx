"use client";

import { useEffect, useMemo, useRef } from "react";
import { AlertTriangle, ExternalLink, GitBranch, Loader2, Sparkles, X } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { api } from "@multica/core/api";
import { cn } from "@multica/ui/lib/utils";
import { useDuplicateCheck } from "../core/queries";
import type { DuplicateMatch } from "../core/types";

// Stable empty array so onMatchesChange consumers (FIR-2550) don't get a
// fresh reference on every render and end up in a setState→re-render loop.
const EMPTY_MATCHES: DuplicateMatch[] = [];

/**
 * DuplicateCheckPanel — "Findes det her allerede?" panel inside the
 * create-issue modal (FIR-2504).
 *
 * Renders nothing unless the cerebro_duplicate_check_on_create flag is on
 * AND the gateway returned ≥ 1 actionable match. We deliberately stay
 * invisible while loading or empty: the panel should help, never shout.
 *
 * Adoption metric: the parent passes `onOpen` and `onAttachAsSubIssue`
 * callbacks so the modal can record (and react to) the user's choice.
 */
export function DuplicateCheckPanel({
  title,
  description,
  projectId,
  onOpen,
  onAttachAsSubIssue,
  variant = "inline",
  onDismiss,
  onMatchesChange,
}: {
  title: string;
  description?: string;
  projectId?: string;
  onOpen: (match: DuplicateMatch) => void;
  /**
   * Optional. When omitted the "Opret som under-issue" action is hidden — used
   * by the agent-create flow (FIR-2550) where the modal closes immediately
   * after submit, so there's no place to thread a parent through.
   */
  onAttachAsSubIssue?: (match: DuplicateMatch) => void;
  /**
   * "inline" (default): panel renders in normal document flow with the legacy
   *   look (mx-5 + muted card). Used in older mounts.
   * "overlay" (FIR-2536): panel renders as a solid card meant to be absolutely
   *   positioned by the parent over the action buttons. Skips the loading-row
   *   UI so the buttons stay visible until there's actually something to act
   *   on, and exposes a dismiss affordance so the user can hide matches and
   *   submit anyway.
   */
  variant?: "inline" | "overlay";
  /** Called when the user clicks the dismiss (X) button (overlay variant). */
  onDismiss?: () => void;
  /**
   * Optional. Fired whenever the actionable match list changes — used by the
   * agent-create flow (FIR-2550) to disable the Submit button while strong
   * matches are visible, so the user must explicitly Open, Dismiss, or choose
   * "create new" before the issue is enqueued.
   */
  onMatchesChange?: (matches: DuplicateMatch[]) => void;
}) {
  const flagOn = useFeatureFlag("cerebro_duplicate_check_on_create");
  const query = useDuplicateCheck({
    title,
    description,
    projectId,
    enabled: flagOn,
  });

  const matches = useMemo(() => query.data ?? EMPTY_MATCHES, [query.data]);
  const shownKeyRef = useRef<string | null>(null);
  // Adoption telemetry (FIR-2504): fire `dismissed` for any previously-shown
  // match set the user moved past without acting on. Server-side `shown` is
  // logged in CheckSimilarIssues; we only need the user-action side here.
  useEffect(() => {
    if (!flagOn) return;
    if (matches.length === 0) return;
    const key = matches.map((m) => m.id).join(",");
    shownKeyRef.current = key;
  }, [flagOn, matches]);
  // FIR-2550: notify the parent of the current match set so the agent-create
  // modal can gate its Submit button on "no strong matches visible". Fires
  // even when the flag is off (callers want to see the empty list and unblock
  // submit) — bail out of the panel render path further down, not here.
  useEffect(() => {
    onMatchesChange?.(matches);
  }, [matches, onMatchesChange]);

  if (!flagOn) return null;

  const isOverlay = variant === "overlay";
  // Inline variant keeps the original loading hint; overlay variant hides
  // loading so the user can still see + use the buttons while we check.
  const showLoading = !isOverlay && matches.length === 0 && (query.isFetching || query.isLoading);
  if (matches.length === 0 && !showLoading) return null;

  return (
    <div
      data-testid="cerebro-duplicate-check-panel"
      data-loading={showLoading ? "true" : "false"}
      data-variant={variant}
      className={cn(
        "text-sm",
        isOverlay
          ? "rounded-t-lg border-t border-x border-border bg-popover px-3 py-2 shadow-lg"
          : "mx-5 mb-2 rounded-lg border border-border bg-muted/40 px-3 py-2",
      )}
    >
      <div className="mb-2 flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        {showLoading ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
        ) : (
          <Sparkles className="h-3.5 w-3.5" />
        )}
        <span className="flex-1">
          {showLoading ? "Tjekker for lignende issues…" : "Findes det her allerede?"}
        </span>
        {isOverlay && onDismiss && (
          <button
            type="button"
            onClick={onDismiss}
            aria-label="Skjul lignende issues"
            className="rounded-sm p-0.5 text-muted-foreground hover:bg-accent/60 hover:text-foreground transition-colors cursor-pointer"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
      <ul className="flex flex-col gap-1.5">
        {matches.map((m) => (
          <MatchRow
            key={m.id}
            match={m}
            onOpen={() => {
              void api.recordCerebroDuplicateCheckEvent({
                action: "opened",
                match_id: m.id,
                verdict: m.verdict,
                match_count: matches.length,
              });
              onOpen(m);
            }}
            onAttachAsSubIssue={
              onAttachAsSubIssue
                ? () => {
                    void api.recordCerebroDuplicateCheckEvent({
                      action: "attached",
                      match_id: m.id,
                      verdict: m.verdict,
                      match_count: matches.length,
                    });
                    onAttachAsSubIssue(m);
                  }
                : undefined
            }
          />
        ))}
      </ul>
    </div>
  );
}

function MatchRow({
  match,
  onOpen,
  onAttachAsSubIssue,
}: {
  match: DuplicateMatch;
  onOpen: () => void;
  /** Optional — when omitted the "Opret som under-issue" action is hidden. */
  onAttachAsSubIssue?: () => void;
}) {
  const isDuplicate = match.verdict === "duplicate";
  return (
    <li
      data-testid="cerebro-duplicate-check-match"
      data-verdict={match.verdict}
      className="flex flex-col gap-1 rounded-md bg-background px-2 py-1.5 ring-1 ring-border/60"
    >
      <div className="flex items-center gap-2">
        <span
          className={cn(
            "inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide",
            isDuplicate
              ? "bg-amber-500/20 text-amber-700 dark:text-amber-300"
              : "bg-sky-500/20 text-sky-700 dark:text-sky-300",
          )}
        >
          {isDuplicate ? (
            <>
              <AlertTriangle className="h-3 w-3" />
              Dubletter
            </>
          ) : (
            <>
              <GitBranch className="h-3 w-3" />
              Relateret
            </>
          )}
        </span>
        {match.identifier && (
          <span className="font-mono text-[11px] text-muted-foreground">
            {match.identifier}
          </span>
        )}
        <span className="flex-1 truncate text-foreground">{match.title}</span>
      </div>
      {match.reason && (
        <div className="text-xs text-muted-foreground">{match.reason}</div>
      )}
      <div className="flex items-center gap-2 pt-0.5 text-xs">
        <button
          type="button"
          onClick={onOpen}
          className="inline-flex items-center gap-1 text-foreground underline-offset-2 hover:underline"
        >
          <ExternalLink className="h-3 w-3" />
          Åbn eksisterende
        </button>
        {onAttachAsSubIssue && (
          <>
            <span aria-hidden className="text-muted-foreground">
              ·
            </span>
            <button
              type="button"
              onClick={onAttachAsSubIssue}
              className="inline-flex items-center gap-1 text-foreground underline-offset-2 hover:underline"
            >
              <GitBranch className="h-3 w-3" />
              Opret som under-issue
            </button>
          </>
        )}
      </div>
    </li>
  );
}
