"use client";

import { useState } from "react";
import { Loader2, MessagesSquare } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ApiError, api, dispatchReasonCode } from "@multica/core/api";
import { useWorkspacePaths } from "@multica/core/paths";
import type { AgentTask } from "@multica/core/types";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";

/** Terminal statuses — the only ones a run can be continued from. A live task
 *  still owns its provider session and its working directory, so continuing it
 *  would put two runs in one directory. */
const CONTINUABLE_STATUSES = new Set(["completed", "failed", "cancelled"]);

export function canContinueTaskInChat(task: AgentTask): boolean {
  return CONTINUABLE_STATUSES.has(task.status);
}

interface ContinueInChatButtonProps {
  task: AgentTask;
  className?: string;
  title?: string;
}

/**
 * Compact icon-button that opens a chat continuing a finished background run.
 * Used on every surface that lists agent tasks — the issue execution log and the
 * agent activity tab — so the affordance is identical in both places.
 *
 * The server inherits the task's provider session + work_dir into the new chat
 * session, so the conversation picks up with the context of the run the user was
 * just reading instead of cold-starting.
 *
 * Two things this deliberately does NOT hide:
 *  - When the run had no resumable session (several backends only report theirs
 *    at completion, and a run that failed early may never have recorded one) the
 *    chat still opens, but we warn. Silently landing the user in a fresh
 *    conversation that looks continuous is the worst outcome here.
 *  - A second click reopens the same conversation rather than forking a new one;
 *    the server is idempotent per (task, member) and we just follow its answer.
 */
export function ContinueInChatButton({
  task,
  className,
  title,
}: ContinueInChatButtonProps) {
  const { t } = useT("common");
  const { push } = useNavigation();
  const workspacePaths = useWorkspacePaths();
  const [starting, setStarting] = useState(false);

  const handleClick = async () => {
    if (starting) return;
    setStarting(true);
    try {
      const result = await api.continueTaskInChat(task.id);
      if (!result.session_carried) {
        // Honest degradation: the chat is real and lands in the run's directory,
        // but the agent has no memory of the run.
        toast.warning(t(($) => $.continue_in_chat.no_session));
      }
      push(`${workspacePaths.chat()}?session=${result.chat_session.id}`);
    } catch (e) {
      // Each branch is a real, reachable state rather than defensive padding:
      //  - invocation_not_allowed: watching a private agent's run does not grant
      //    the right to start new ones, so a viewer can hold this button and
      //    still be refused.
      //  - task_not_terminal: the run went live again between render and click.
      //  - already_chat_task: only reachable by calling the endpoint directly,
      //    but the message should still make sense.
      //
      // Note the two different fields: `reason_code` is the shared
      // dispatch-blocked shape that writeDispatchBlocked emits for the 403, so it
      // is read via dispatchReasonCode; the 409s carry a plain `reason`. Not
      // symmetric, but reusing the dispatch enum for a non-admission refusal
      // would be worse.
      const reason =
        e instanceof ApiError && e.body && typeof e.body === "object"
          ? (e.body as { reason?: unknown }).reason
          : undefined;
      toast.error(
        dispatchReasonCode(e) === "invocation_not_allowed"
          ? t(($) => $.continue_in_chat.blocked)
          : reason === "task_not_terminal"
            ? t(($) => $.continue_in_chat.still_running)
            : reason === "already_chat_task"
              ? t(($) => $.continue_in_chat.already_chat)
              : e instanceof Error
                ? e.message
                : t(($) => $.continue_in_chat.failed),
      );
      setStarting(false);
      return;
    }
    // Deliberately leave `starting` true on success: the row stays mounted while
    // the route transition commits, and clearing it would flash the icon back
    // just before navigation.
  };

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            onClick={() => void handleClick()}
            disabled={starting}
            aria-label={t(($) => $.continue_in_chat.aria)}
          />
        }
        className={cn(
          "flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50",
          className,
        )}
      >
        {starting ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
        ) : (
          <MessagesSquare className="h-3.5 w-3.5" />
        )}
      </TooltipTrigger>
      <TooltipContent>
        {title ?? t(($) => $.continue_in_chat.tooltip)}
      </TooltipContent>
    </Tooltip>
  );
}
