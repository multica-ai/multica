"use client";

import { Fragment, useEffect, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight, ChevronDown, ChevronRight, TriangleAlert } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { workflowHandoffPreviewOptions } from "@multica/core/issue-workflows";
import { useActorName } from "@multica/core/workspace/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { ActorAvatar } from "../common/actor-avatar";
import { StatusIcon } from "../issues/components/status-icon";
import { useStatusLabel } from "../issues/utils/status-label";
import { useT } from "../i18n";
import { BriefBlock } from "./workflow-editor-page";

const FROM = "⁣from⁣";
const TO = "⁣to⁣";

/** Replaces the FROM/TO markers of an interpolated sentence with nodes. */
function withChips(text: string, chips: { from?: ReactNode; to: ReactNode }): ReactNode[] {
  return text.split(/(⁣from⁣|⁣to⁣)/).map((part, i) => {
    if (part === FROM) return <Fragment key={i}>{chips.from}</Fragment>;
    if (part === TO) return <Fragment key={i}>{chips.to}</Fragment>;
    return <Fragment key={i}>{part}</Fragment>;
  });
}

function useDuration() {
  const { t } = useT("issues");
  return (since: string | null) => {
    if (!since) return t(($) => $.workflows.confirm.under_minute);
    const minutes = Math.floor((Date.now() - new Date(since).getTime()) / 60_000);
    if (minutes < 1) return t(($) => $.workflows.confirm.under_minute);
    if (minutes < 60) return t(($) => $.workflows.confirm.minutes, { count: minutes });
    return t(($) => $.workflows.confirm.hours, { count: Math.floor(minutes / 60) });
  };
}

/**
 * Confirms a status change that hands the issue off (MUL-7420): who it goes
 * from and to, whether the previous agent is still running (with a default-on
 * option to stop it), and the brief the new handler receives. The facts come
 * from the server preview, so they match what the write will do.
 *
 * When the preview finds no handoff after all (the handler cannot be resolved,
 * or the workflow changed), the move applies without asking.
 */
export function WorkflowHandoffConfirmDialog({
  issue,
  toStatus,
  onCancel,
  onConfirm,
}: {
  /** The issue being moved; null keeps the dialog closed. */
  issue: { id: string; identifier: string } | null;
  toStatus: string;
  onCancel: () => void;
  onConfirm: (options: { stopPreviousRuns: boolean }) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const catalog = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);
  const { getActorName } = useActorName();
  const duration = useDuration();
  const [stop, setStop] = useState(true);
  const [briefOpen, setBriefOpen] = useState(true);
  const open = issue !== null;
  const { data: preview, isPending, isError } = useQuery({
    ...workflowHandoffPreviewOptions(wsId, issue?.id ?? "", toStatus),
    enabled: open,
  });

  useEffect(() => {
    if (open) {
      setStop(true);
      setBriefOpen(true);
    }
  }, [open, issue?.id, toStatus]);

  // Nothing to confirm: apply as an ordinary status change.
  useEffect(() => {
    if (open && preview && !preview.handoff) onConfirm({ stopPreviousRuns: false });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, preview]);

  const status = (key: string) => (
    <span className="inline-flex items-center gap-1">
      <StatusIcon
        status={key}
        category={catalog.categoryOf(key)}
        color={catalog.colorOf(key)}
        icon={catalog.iconOf(key)}
        className="h-3 w-3"
      />
      {labelOf(key)}
    </span>
  );
  const chip = (type: string, id: string) => (
    <span className="mx-0.5 inline-flex items-center gap-1.5 align-middle font-medium">
      <ActorAvatar actorType={type} actorId={id} size="xs" profileLink={false} />
      {getActorName(type, id)}
    </span>
  );

  const handlerType = preview?.handler_type ?? null;
  const handlerId = preview?.handler_id ?? null;
  const handlerName = handlerType && handlerId ? getActorName(handlerType, handlerId) : "";
  const prevType = preview?.previous_assignee_type ?? null;
  const prevId = preview?.previous_assignee_id ?? null;
  const sameHandler = !!prevId && prevId === handlerId && prevType === handlerType;
  const runs = preview?.previous_runs ?? [];
  // A queued run has not started yet: stopping it cancels rather than
  // interrupts, and there is no elapsed time to show.
  const started = runs.some((run) => run.status === "running" || run.status === "dispatched");
  const oldestRun = runs.reduce<string | null>(
    (oldest, run) => (!oldest || (run.started_at && run.started_at < oldest) ? run.started_at : oldest),
    null,
  );
  const prevName = prevType && prevId ? getActorName(prevType, prevId) : "";

  let sentence: ReactNode = null;
  if (preview?.handoff && handlerType && handlerId) {
    if (sameHandler) {
      sentence = withChips(t(($) => $.workflows.confirm.same_handler_run, { to: TO }), { to: chip(handlerType, handlerId) });
    } else {
      const text =
        handlerType === "member"
          ? t(($) => $.workflows.confirm.reassign, { from: FROM, to: TO })
          : t(($) => $.workflows.confirm.reassign_run, { from: FROM, to: TO });
      sentence = withChips(text, {
        from: prevType && prevId ? chip(prevType, prevId) : <span className="font-medium">{t(($) => $.workflows.confirm.unassigned)}</span>,
        to: chip(handlerType, handlerId),
      });
    }
  }

  return (
    <Dialog open={open && !(preview && !preview.handoff)} onOpenChange={(v) => !v && onCancel()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {t(($) => $.workflows.confirm.title, { issue: issue?.identifier ?? "", status: labelOf(toStatus) })}
          </DialogTitle>
          <DialogDescription className="flex flex-wrap items-center gap-1.5">
            {preview && status(preview.from_status)}
            {preview && <ArrowRight aria-hidden className="size-3" />}
            {status(toStatus)}
            {preview?.workflow_name && (
              <>
                <span aria-hidden className="text-muted-foreground/50">·</span>
                <span>{preview.workflow_name}</span>
              </>
            )}
          </DialogDescription>
        </DialogHeader>

        {isPending ? (
          <div className="flex justify-center py-6">
            <Spinner className="size-4" />
          </div>
        ) : isError ? (
          <p className="text-body text-destructive">{t(($) => $.workflows.confirm.error)}</p>
        ) : (
          <div className="space-y-4">
            {sentence && <div className="text-body leading-7">{sentence}</div>}
            {runs.length > 0 && !sameHandler && (
              <div className="rounded-lg border border-warning/30 bg-warning/10 px-3.5 py-3">
                <p className="flex items-center gap-2 text-body font-medium">
                  <TriangleAlert aria-hidden className="size-3.5 shrink-0" />
                  {started
                    ? t(($) => $.workflows.confirm.running_title, { name: prevName, duration: duration(oldestRun) })
                    : t(($) => $.workflows.confirm.queued_title, { name: prevName })}
                </p>
                <label className="mt-2 flex cursor-pointer items-center gap-2 text-body">
                  <Checkbox checked={stop} onCheckedChange={(v) => setStop(v === true)} />
                  {started
                    ? t(($) => $.workflows.confirm.stop_label, { name: prevName })
                    : t(($) => $.workflows.confirm.cancel_queued_label, { name: prevName })}
                </label>
                <p className="ml-6 mt-1 text-caption leading-5 text-muted-foreground">
                  {started
                    ? t(($) => $.workflows.confirm.stop_hint)
                    : t(($) => $.workflows.confirm.queued_hint, { name: prevName })}
                </p>
              </div>
            )}
            {preview?.brief && (
              <div>
                <button
                  type="button"
                  onClick={() => setBriefOpen((v) => !v)}
                  aria-expanded={briefOpen}
                  className="flex items-center gap-1 text-caption font-medium text-brand hover:underline"
                >
                  {briefOpen ? <ChevronDown aria-hidden className="size-3.5" /> : <ChevronRight aria-hidden className="size-3.5" />}
                  {t(($) => $.workflows.confirm.brief_toggle, { name: handlerName })}
                </button>
                {briefOpen && (
                  <div className="mt-2">
                    <BriefBlock brief={preview.brief} />
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            {t(($) => $.workflows.confirm.cancel)}
          </Button>
          <Button
            disabled={isPending || isError}
            onClick={() => onConfirm({ stopPreviousRuns: stop && runs.length > 0 && !sameHandler })}
          >
            {t(($) => $.workflows.confirm.confirm, { status: labelOf(toStatus) })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
