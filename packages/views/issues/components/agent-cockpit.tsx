"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  CornerDownRight,
  Loader2,
  RotateCcw,
  Send,
  Square,
  SquareTerminal,
} from "lucide-react";
import { toast } from "sonner";
import { api, dispatchReasonCode } from "@multica/core/api";
import {
  useCancelIssueTask,
  useCreateComment,
  useRerunIssueTask,
} from "@multica/core/issues/mutations";
import { unhandledCommentTriggerOutcomes } from "@multica/core/issues/comment-trigger-outcomes";
import type { AgentTask } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { TranscriptButton } from "../../common/task-transcript";
import { useT } from "../../i18n";
import { AgentTerminal } from "./agent-terminal";
import { TerminateTaskConfirmDialog } from "./terminate-task-confirm-dialog";

function isActiveTask(task: AgentTask): boolean {
  return task.status === "queued" ||
    task.status === "dispatched" ||
    task.status === "waiting_local_directory" ||
    task.status === "running";
}

function escapeMentionLabel(label: string): string {
  return label.replaceAll("\\", "\\\\").replaceAll("[", "\\[").replaceAll("]", "\\]");
}

export function AgentCockpitLauncher({
  runningCount,
  onOpen,
}: {
  runningCount: number;
  onOpen: () => void;
}) {
  const { t } = useT("agents");
  return (
    <Button
      type="button"
      variant="brandSubtle"
      size="sm"
      className="mb-1.5 w-full justify-start"
      onClick={onOpen}
    >
      <SquareTerminal className="h-4 w-4" />
      <span>{t(($) => $.cockpit.open_terminal)}</span>
      <span className="ml-auto inline-flex items-center gap-1 font-mono text-micro tabular-nums text-info">
        <span className="h-1.5 w-1.5 rounded-full bg-info animate-pulse" />
        {runningCount}
      </span>
    </Button>
  );
}

export function AgentCockpitIconButton({ onOpen }: { onOpen: () => void }) {
  const { t } = useT("agents");
  const label = t(($) => $.cockpit.open_terminal);
  return (
    <Tooltip>
      <TooltipTrigger
        render={<button type="button" onClick={onOpen} aria-label={label} />}
        className="flex items-center justify-center rounded p-1 text-info transition-colors hover:bg-info/10"
      >
        <SquareTerminal className="h-3.5 w-3.5" />
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

export function AgentCockpitSession({
  task,
  issueId,
  open,
  onOpenChange,
  allowTerminalContinuation = true,
}: {
  task: AgentTask;
  issueId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  allowTerminalContinuation?: boolean;
}) {
  const { t } = useT("agents");
  const { data: terminal } = useQuery({
    queryKey: ["task-terminal", task.id],
    queryFn: () => api.getTaskTerminal(task.id),
    enabled: open,
    refetchInterval: (query) =>
      query.state.data?.status === "running" || query.state.data?.status === "reconnecting"
        ? 3_000
        : false,
  });

  return (
    <TranscriptButton
      task={task}
      agentName=""
      isLive={task.status === "running"}
      variant="cockpit"
      renderButton={false}
      open={open}
      onOpenChange={onOpenChange}
      terminalSlot={
        terminal?.available ? <AgentTerminal taskId={task.id} metadata={terminal} /> : undefined
      }
      headerSlot={
        terminal && !terminal.available && terminal.session_id ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.cockpit.terminal_replay_unavailable)}
          </p>
        ) : undefined
      }
      headerActions={({ agentName }) => (
        <AgentCockpitActions
          task={task}
          issueId={issueId}
          agentName={agentName}
          allowTerminalContinuation={allowTerminalContinuation}
          terminalRun={
            terminal?.available === true ||
            terminal?.mode === "pty" ||
            Boolean(terminal?.session_id)
          }
          onSessionClose={() => onOpenChange(false)}
        />
      )}
    />
  );
}

function AgentCockpitActions({
  task,
  issueId,
  agentName,
  allowTerminalContinuation,
  terminalRun,
  onSessionClose,
}: {
  task: AgentTask;
  issueId: string;
  agentName: string;
  allowTerminalContinuation: boolean;
  terminalRun: boolean;
  onSessionClose: () => void;
}) {
  const { t } = useT("agents");
  const createComment = useCreateComment(issueId);
  const cancelTask = useCancelIssueTask(issueId);
  const rerunTask = useRerunIssueTask(issueId);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [redirectOpen, setRedirectOpen] = useState(false);
  const [instruction, setInstruction] = useState("");
  const [redirecting, setRedirecting] = useState(false);
  const [stopping, setStopping] = useState(false);
  const active = isActiveTask(task);
  const canRestart = task.status === "failed" || task.status === "cancelled";

  const handleStop = async () => {
    if (cancelTask.isPending || stopping) return;
    let cancelRequested = false;
    setStopping(true);
    try {
      await cancelTask.mutateAsync(task.id);
      cancelRequested = true;
      setConfirmOpen(false);
      await cancelTask.waitForAcknowledgement(task.id, task.status);
      toast.success(t(($) => $.cockpit.stop_success));
    } catch (error) {
      toast.error(
        cancelRequested
          ? t(($) => $.cockpit.stop_unconfirmed)
          : error instanceof Error && error.message
          ? error.message
          : t(($) => $.cockpit.stop_failed),
      );
    } finally {
      setStopping(false);
    }
  };

  const handleRestart = async () => {
    if (rerunTask.isPending) return;
    try {
      await rerunTask.mutateAsync(task.id);
      toast.success(t(($) => $.cockpit.restart_success));
      onSessionClose();
    } catch (error) {
      toast.error(
        dispatchReasonCode(error) === "invocation_not_allowed"
          ? t(($) => $.cockpit.restart_blocked)
          : error instanceof Error && error.message
            ? error.message
            : t(($) => $.cockpit.restart_failed),
      );
    }
  };

  const handleRedirect = async (event: React.FormEvent) => {
    event.preventDefault();
    const trimmed = instruction.trim();
    if (!trimmed || redirecting || !task.agent_id) return;

    setRedirecting(true);
    let cancelRequested = false;
    let stopConfirmed = !active;
    try {
      if (active) {
        await cancelTask.mutateAsync(task.id);
        cancelRequested = true;
        await cancelTask.waitForAcknowledgement(task.id, task.status);
        stopConfirmed = true;
      }
      const label = escapeMentionLabel(agentName || t(($) => $.cockpit.agent_fallback));
      const comment = await createComment.mutateAsync({
        content: `[@${label}](mention://agent/${task.agent_id}) ${trimmed}`,
      });
      const outcome = unhandledCommentTriggerOutcomes(comment.trigger_outcomes).find(
        (candidate) => candidate.target_type === "agent" && candidate.target_id === task.agent_id,
      );
      if (outcome) {
        toast.warning(t(($) => $.cockpit.redirect_blocked));
      } else {
        toast.success(t(($) => $.cockpit.redirect_success));
      }
      setInstruction("");
      setRedirectOpen(false);
      onSessionClose();
    } catch (error) {
      toast.error(
        cancelRequested && !stopConfirmed
          ? t(($) => $.cockpit.redirect_stop_unconfirmed)
          : active && stopConfirmed
            ? t(($) => $.cockpit.redirect_send_failed_after_stop)
            : error instanceof Error && error.message
            ? error.message
            : t(($) => $.cockpit.redirect_failed),
      );
    } finally {
      setRedirecting(false);
    }
  };

  return (
    <>
      {!terminalRun && (active || allowTerminalContinuation) && (
        <Popover open={redirectOpen} onOpenChange={setRedirectOpen}>
          <PopoverTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                size="xs"
                aria-label={active
                  ? t(($) => $.cockpit.redirect_agent)
                  : t(($) => $.cockpit.continue_agent)}
              />
            }
          >
            <CornerDownRight className="h-3.5 w-3.5" />
            {active
              ? t(($) => $.cockpit.redirect_agent)
              : t(($) => $.cockpit.continue_agent)}
          </PopoverTrigger>
          <PopoverContent align="end" className="w-96 max-w-[calc(100vw-2rem)] p-3">
            <form className="space-y-3" onSubmit={handleRedirect}>
              <div>
                <div className="text-caption font-medium text-foreground">
                  {active
                    ? t(($) => $.cockpit.redirect_title)
                    : t(($) => $.cockpit.continue_title)}
                </div>
                <p className="mt-1 text-micro leading-relaxed text-muted-foreground">
                  {active
                    ? t(($) => $.cockpit.redirect_description)
                    : t(($) => $.cockpit.continue_description)}
                </p>
              </div>
              <Textarea
                value={instruction}
                onChange={(event) => setInstruction(event.target.value)}
                placeholder={t(($) => $.cockpit.instruction_placeholder)}
                disabled={redirecting}
                className="min-h-24 resize-y"
                autoFocus
              />
              <div className="flex justify-end gap-2">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setRedirectOpen(false)}
                  disabled={redirecting}
                >
                  {t(($) => $.cockpit.cancel)}
                </Button>
                <Button type="submit" size="sm" disabled={!instruction.trim() || redirecting}>
                  {redirecting ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Send className="h-3.5 w-3.5" />
                  )}
                  {active
                    ? t(($) => $.cockpit.stop_and_continue)
                    : t(($) => $.cockpit.continue_with_instruction)}
                </Button>
              </div>
            </form>
          </PopoverContent>
        </Popover>
      )}

      {active && (
        <Button
          type="button"
          variant="destructive"
          size="xs"
          onClick={() => setConfirmOpen(true)}
          disabled={cancelTask.isPending || redirecting || stopping}
        >
          {cancelTask.isPending || stopping ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Square className="h-3.5 w-3.5" />
          )}
          {t(($) => $.cockpit.stop_agent)}
        </Button>
      )}

      {canRestart && (
        <Button
          type="button"
          variant="outline"
          size="xs"
          onClick={() => void handleRestart()}
          disabled={rerunTask.isPending || redirecting}
        >
          {rerunTask.isPending ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <RotateCcw className="h-3.5 w-3.5" />
          )}
          {t(($) => $.cockpit.restart_agent)}
        </Button>
      )}

      <TerminateTaskConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        onConfirm={() => void handleStop()}
        showRunningNote={task.status === "running" || task.status === "dispatched"}
      />
    </>
  );
}
