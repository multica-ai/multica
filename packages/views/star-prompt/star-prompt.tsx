"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Star, X } from "@multica/ui/icons";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { useConfigStore } from "@multica/core/config";
import { formatStarCount, githubStarsOptions, MULTICA_GITHUB_URL } from "@multica/core/github/stars";
import { createLogger } from "@multica/core/logger";
import { needsSourceBackfill } from "@multica/core/onboarding";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useSessionPromptStore } from "@multica/core/session-prompt";
import {
  agentDoneIssueCountOptions,
  claimStarPromptShowing,
  markStarPromptStarred,
  STAR_PROMPT_MIN_AGENT_DONE_ISSUES,
  starPromptDue,
  useStarPromptStore,
  type StarPromptMoment,
} from "@multica/core/star-prompt";
import { agentListOptions } from "@multica/core/workspace/queries";
import { isFloatingChatRouteSuppressed } from "../chat/floating-chat-visibility";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { GitHubIcon } from "../onboarding/components/brand-icons";
import { readSourceBackfillDismissCount } from "../onboarding/source-backfill-dismiss";

const logger = createLogger("star-prompt");

/** Lets the status change settle before the card slides in. */
export const STAR_PROMPT_DELAY_MS = 2500;

/**
 * Nothing is open or being typed into. A card that appears mid-sentence or
 * over a dialog reads as an interruption, so such moments are skipped rather
 * than deferred.
 */
function isQuietMoment(): boolean {
  if (document.visibilityState !== "visible") return false;
  if (document.querySelector("[role='dialog'][aria-modal='true'], [role='alertdialog']")) return false;
  const active = document.activeElement;
  if (
    active instanceof HTMLElement &&
    (active.isContentEditable || active.tagName === "INPUT" || active.tagName === "TEXTAREA")
  ) {
    return false;
  }
  return true;
}

interface Showing {
  agentName: string;
  doneCount: number;
}

/**
 * "Star us on GitHub" card. Appears a moment after the user moves an agent's
 * issue to done, once that agent has finished enough issues to have earned it.
 * The server decides whether a showing is still due (account age, cooldown,
 * showings used); this component decides whether the moment is right.
 */
export function StarPrompt() {
  const { t } = useT("layout");
  const titleId = useId();
  // Shells mount this while the workspace slug is still resolving.
  const wsId = useCurrentWorkspace()?.id ?? null;
  const qc = useQueryClient();
  const moment = useStarPromptStore((s) => s.moment);
  const disabled = useConfigStore((s) => s.starPromptDisabled);
  const chatOpen = useChatStore((s) => s.isOpen);
  const floatingChatEnabled = useChatStore((s) => s.floatingChatEnabled);
  const { pathname } = useNavigation();
  const chatPath = useWorkspacePaths().chat();
  const launcherVisible =
    floatingChatEnabled && !chatOpen && !isFloatingChatRouteSuppressed(pathname, chatPath);
  const [showing, setShowing] = useState<Showing | null>(null);
  const { data: stars } = useQuery({ ...githubStarsOptions(), enabled: showing !== null });

  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    if (!moment || !wsId) return;

    const consider = async (m: StarPromptMoment, workspaceId: string) => {
      if (m.wsId !== workspaceId || disabled) return;
      if (useSessionPromptStore.getState().prompted) return;
      const user = useAuthStore.getState().user;
      if (!starPromptDue(user, Date.now())) return;
      // The attribution survey owns this session's one interruption.
      if (needsSourceBackfill(user, readSourceBackfillDismissCount(user?.id))) return;
      if (!isQuietMoment()) return;

      const [doneCount, agents] = await Promise.all([
        qc.fetchQuery(agentDoneIssueCountOptions(workspaceId, m.agentId)),
        qc.fetchQuery(agentListOptions(workspaceId)),
      ]);
      const agent = agents.find((a) => a.id === m.agentId);
      if (!agent || doneCount < STAR_PROMPT_MIN_AGENT_DONE_ISSUES) return;
      if (!mountedRef.current || !(await claimStarPromptShowing())) return;
      useSessionPromptStore.getState().markPrompted();
      if (mountedRef.current) setShowing({ agentName: agent.name, doneCount });
    };

    const timer = window.setTimeout(() => {
      useStarPromptStore.getState().clearMoment();
      consider(moment, wsId).catch((err) => logger.warn("failed to evaluate star prompt", err));
    }, STAR_PROMPT_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [moment, wsId, disabled, qc]);

  // The chat window covers this corner while it is open.
  if (!showing || chatOpen) return null;

  const close = () => setShowing(null);
  const star = () => {
    close();
    markStarPromptStarred().catch((err) => logger.warn("failed to record star click", err));
  };

  return (
    <section
      aria-labelledby={titleId}
      aria-live="polite"
      data-slot="star-prompt"
      className={cn(
        "absolute right-[var(--chat-launcher-inset)] z-50 w-[min(23.75rem,calc(100%-2*var(--chat-launcher-inset)))] rounded-xl bg-surface-raised p-4 pb-3 text-popover-foreground shadow-[var(--floating-shadow)] ring-1 ring-surface-border",
        "animate-in fade-in-0 slide-in-from-bottom-2 duration-200 motion-reduce:animate-none",
        launcherVisible ? "above-chat-launcher" : "bottom-[var(--chat-launcher-inset)]",
      )}
    >
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label={t(($) => $.star_prompt.close)}
        onClick={close}
        className="absolute top-2 right-2 text-muted-foreground"
      >
        <X />
      </Button>
      <div className="flex items-start gap-2 pr-6">
        <Star aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-brand" />
        <div className="min-w-0">
          <h2 id={titleId} className="text-body font-semibold [overflow-wrap:anywhere]">
            {t(($) => $.star_prompt.title, { agent: showing.agentName, count: showing.doneCount })}
          </h2>
          <p className="mt-1 text-label text-muted-foreground">{t(($) => $.star_prompt.body)}</p>
        </div>
      </div>
      <div className="mt-3 flex items-center justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={close}>
          {t(($) => $.star_prompt.later)}
        </Button>
        <a
          href={MULTICA_GITHUB_URL}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={t(($) => $.star_prompt.star_label)}
          onClick={star}
          className={buttonVariants({ size: "sm" })}
        >
          <GitHubIcon className="size-3.5" />
          {t(($) => $.star_prompt.star)}
          {typeof stars === "number" && (
            <span aria-hidden="true" className="inline-flex items-center gap-1.5 tabular-nums">
              <span className="h-3 w-px bg-current opacity-30" />
              {formatStarCount(stars)}
            </span>
          )}
        </a>
      </div>
    </section>
  );
}
