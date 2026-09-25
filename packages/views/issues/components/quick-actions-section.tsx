"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Loader2, Zap } from "lucide-react";
import { toast } from "sonner";
import { dispatchReasonCode } from "@multica/core/api";
import { useCurrentWorkspace } from "@multica/core/paths";
import { quickActionListOptions, useRunQuickAction } from "@multica/core/quick-actions";
import type { Comment, CommentTriggerOutcome, QuickAction } from "@multica/core/types";
import { QUICK_ACTION_SIDEBAR_LIMIT } from "@multica/core/types";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

// Quick Actions sidebar section (MUL-5465).
//
// The list is NOT filtered by invoke permission. An earlier version hid
// actions the viewer could not run, which meant two people on one issue saw
// different sidebars with nothing to explain the difference — harder to debug
// than a button that tells you why it refused. Permission is answered once, by
// the run endpoint; a refusal opens a dialog.
//
// One interaction: click runs it. There is no per-action input field — the `/`
// slash command already drops a rendered action into the composer for editing,
// which covers "same action, one detail different" more flexibly than a single
// fixed field could, so a second UI for it was removed.
//
// The result toast is deliberately outcome-specific rather than a generic
// "done". A second click against an agent that already has a pending task on
// this issue does NOT start a second run — the DB permits one pending task per
// (issue, agent) and the comment is merged into the existing one. Reporting
// that as success would be a lie the user later has to debug.

export function QuickActionsSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  // Nullable read, not useWorkspaceId(): this section is an enhancement on the
  // sidebar and must degrade to "not rendered" outside a workspace route
  // rather than throwing and taking the whole issue detail down with it.
  const wsId = useCurrentWorkspace()?.id ?? "";
  const [open, setOpen] = useState(true);
  const [showAll, setShowAll] = useState(false);
  const [blocked, setBlocked] = useState<QuickAction | null>(null);

  const { data = [], isLoading } = useQuery({
    ...quickActionListOptions(wsId),
    enabled: wsId !== "",
  });
  const actions = useMemo(() => data.filter((a) => a.status === "active"), [data]);

  // Nothing configured renders nothing at all: an empty section header in the
  // sidebar is pure noise.
  if (isLoading || actions.length === 0) return null;

  const visible = showAll ? actions : actions.slice(0, QUICK_ACTION_SIDEBAR_LIMIT);
  const hiddenCount = actions.length - visible.length;

  return (
    <div>
      <button
        type="button"
        className={cn(
          "mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70",
          !open && "text-muted-foreground hover:text-foreground",
        )}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.detail.section_quick_actions)}
        <ChevronRight
          className={cn(
            "!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
      </button>
      {open ? (
        <div className="space-y-0.5 pl-2">
          {visible.map((action) => (
            <QuickActionRow
              key={action.id}
              action={action}
              issueId={issueId}
              onBlocked={() => setBlocked(action)}
            />
          ))}
          {hiddenCount > 0 ? (
            <button
              type="button"
              className="w-full rounded-md px-2 py-1 text-left text-caption text-muted-foreground transition-colors hover:bg-accent/70 hover:text-foreground"
              onClick={() => setShowAll(true)}
            >
              {t(($) => $.detail.quick_actions_more, { count: hiddenCount })}
            </button>
          ) : null}
        </div>
      ) : null}

      {/* One message for every refusal. The person reading this dialog and the
          person who can fix the binding are different people — the clicker's
          next step is the same either way, and the diagnostic detail lives in
          settings where the owner will act on it. */}
      <AlertDialog open={blocked !== null} onOpenChange={(v) => !v && setBlocked(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.detail.quick_action_blocked_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.detail.quick_action_blocked_body, { name: blocked?.name ?? "" })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogAction>{t(($) => $.detail.quick_action_blocked_ok)}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/**
 * Translate the per-target outcome into an honest sentence.
 *
 * `coalesced` is the one that matters: the click was accepted but no NEW run
 * started. "Added to Lambda's current run" is the truth; "Lambda started
 * working" is not.
 */
function outcomeMessage(
  outcome: CommentTriggerOutcome | undefined,
  targetName: string,
  t: ReturnType<typeof useT<"issues">>["t"],
): { message: string; kind: "success" | "info" | "error" } {
  if (!outcome) {
    // No outcome at all means the comment saved but nothing was reported —
    // surface it as neutral rather than claiming a run.
    return { message: t(($) => $.detail.quick_action_posted), kind: "info" };
  }
  switch (outcome.status) {
    case "queued":
      return { message: t(($) => $.detail.quick_action_queued, { name: targetName }), kind: "success" };
    case "coalesced":
      return { message: t(($) => $.detail.quick_action_coalesced, { name: targetName }), kind: "info" };
    case "deferred":
      return { message: t(($) => $.detail.quick_action_deferred, { name: targetName }), kind: "info" };
    case "blocked":
      return { message: t(($) => $.detail.quick_action_blocked, { name: targetName }), kind: "error" };
    default:
      // Server-driven enum: an unknown status must not be silently rendered
      // as success.
      return { message: t(($) => $.detail.quick_action_posted), kind: "info" };
  }
}

function QuickActionRow({
  action,
  issueId,
  onBlocked,
}: {
  action: QuickAction;
  issueId: string;
  onBlocked: () => void;
}) {
  const { t } = useT("issues");
  const runMutation = useRunQuickAction(issueId);
  const running = runMutation.isPending;

  const targetName = action.target_name || t(($) => $.detail.quick_action_target_fallback);

  const handleRun = async () => {
    try {
      const comment: Comment = await runMutation.mutateAsync({ quickActionId: action.id });
      const { message, kind } = outcomeMessage(comment.trigger_outcomes?.[0], targetName, t);
      if (kind === "success") toast.success(message);
      else if (kind === "error") toast.error(message);
      else toast.info(message);
    } catch (error) {
      // A permission refusal is a structured 403 the user needs explained, not
      // a transient failure they should retry — it gets the dialog. Everything
      // else stays a toast.
      if (dispatchReasonCode(error) === "invocation_not_allowed") {
        onBlocked();
        return;
      }
      toast.error(error instanceof Error ? error.message : String(error));
    }
  };

  const row = (
    <div>
      <button
        type="button"
        disabled={running}
        onClick={() => void handleRun()}
        className={cn(
          "group flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body transition-colors",
          "hover:bg-accent/70 disabled:cursor-default disabled:opacity-60",
        )}
      >
        {running ? (
          <Loader2 className="size-3.5 shrink-0 animate-spin text-muted-foreground" />
        ) : (
          <Zap className="size-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="min-w-0 flex-1 truncate">{action.name}</span>
        {action.target_name ? (
          <ActorAvatar
            actorType={action.assignee_type === "squad" ? "squad" : "agent"}
            actorId={action.assignee_id}
            size="xs"
            className="shrink-0"
            showStatusDot={action.assignee_type === "agent"}
          />
        ) : null}
      </button>
    </div>
  );

  return (
    <Tooltip>
      <TooltipTrigger render={row} />
      <TooltipContent side="left">
        <div className="space-y-0.5">
          <div className="font-medium">{action.name}</div>
          {action.description ? <div>{action.description}</div> : null}
          <div className="text-muted-foreground">
            {t(($) => $.detail.quick_action_runs_as, { name: targetName })}
          </div>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}
