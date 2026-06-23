"use client";

import { useState, type ReactNode } from "react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Spinner } from "@multica/ui/components/ui/spinner";
import type { IssueAssigneeType, IssueStatus, UpdateIssueRequest } from "@multica/core/types";
import { useUpdateIssue, useBatchUpdateIssues } from "@multica/core/issues/mutations";
import { useActorName } from "@multica/core/workspace/hooks";
import { useIssueTriggerPreview } from "../issues/hooks/use-issue-trigger-preview";
import { useT } from "../i18n";

const MAX_HANDOFF_NOTE = 2000;

// i18next inlines {{name}} into the sentence, but the actor's position varies by
// language ("{{name}} 会…" vs "Once assigned, {{name}} will…" vs "{{name}}'s
// leader…"). Fence the name with a sentinel so we can bold just that span at
// render time without splitting copy into per-language prefix/suffix keys.
const NAME_FENCE = "\u0000";

function boldName(text: string): ReactNode {
  const parts = text.split(NAME_FENCE);
  if (parts.length !== 3) return text;
  return (
    <>
      {parts[0]}
      <span className="font-semibold text-foreground">{parts[1]}</span>
      {parts[2]}
    </>
  );
}

interface RunConfirmData {
  issueIds?: string[];
  mode?: "assign" | "status";
  assigneeType?: IssueAssigneeType;
  assigneeId?: string;
  assigneeName?: string;
  status?: IssueStatus;
}

/**
 * Pre-trigger confirmation for issue writes that may start agent runs
 * (MUL-3375 §4). Shows what the unified backend predicate says will start (via
 * the preview endpoint — never a frontend guess), lets the user attach a
 * handoff note (assign only) and choose "暂不开始", then applies the change.
 * Dismissing the dialog (X / Esc / click-outside) cancels without any write.
 * Shared by single assign (1 id) and batch assign / batch status (N ids).
 */
export function RunConfirmModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const { getActorName } = useActorName();
  const d = (data ?? {}) as RunConfirmData;
  const issueIds = d.issueIds ?? [];
  const mode = d.mode ?? "assign";

  const [note, setNote] = useState("");
  // Which footer action is in flight, so only the clicked button shows a
  // spinner (the request runs an agent on the server for note assigns, so it is
  // not instant — the disabled-only state read as frozen).
  const [pendingAction, setPendingAction] = useState<"go" | "suppress" | null>(null);
  const submitting = pendingAction !== null;

  const updateIssue = useUpdateIssue();
  const batchUpdate = useBatchUpdateIssues();

  const preview = useIssueTriggerPreview({
    issueIds,
    assigneeType: d.assigneeType ?? null,
    assigneeId: d.assigneeId ?? null,
    status: d.status,
    enabled: issueIds.length > 0,
  });

  const loading = preview.isLoading;
  const willStart = preview.totalCount > 0;
  const canNote = mode === "assign" && willStart;
  // Soft gate: an old runtime can't render the note. Disable the box but let
  // the assignment proceed (MUL-3375 §6.3).
  const noteDisabled = canNote && !preview.handoffSupported;

  const applyTo = (extra: Partial<UpdateIssueRequest>) => {
    const base: UpdateIssueRequest =
      mode === "assign"
        ? { assignee_type: d.assigneeType ?? null, assignee_id: d.assigneeId ?? null }
        : { status: d.status };
    return { ...base, ...extra };
  };

  const submit = async (suppressRun: boolean) => {
    if (issueIds.length === 0 || submitting) return;
    setPendingAction(suppressRun ? "suppress" : "go");
    const payload = applyTo({
      ...(suppressRun ? { suppress_run: true } : {}),
      ...(!suppressRun && canNote && !noteDisabled && note.trim()
        ? { handoff_note: note.trim() }
        : {}),
    });
    try {
      if (issueIds.length === 1) {
        await updateIssue.mutateAsync({ id: issueIds[0]!, ...payload });
      } else {
        await batchUpdate.mutateAsync({ ids: issueIds, updates: payload });
      }
      onClose();
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.run_confirm.toast_failed));
      setPendingAction(null);
    }
  };

  // A squad doesn't "work" — its leader evaluates the issue and delegates. The
  // copy reflects that (see issues.json squad_leader_*). Only knowable in assign
  // mode, where assigneeType is carried; status-mode triggers expose only the
  // resolved leader agent, so they stay on the generic copy.
  const isSquad = mode === "assign" && d.assigneeType === "squad";

  const headline: ReactNode = (() => {
    if (!willStart) {
      return mode === "assign"
        ? t(($) => $.run_confirm.nothing_assign)
        : t(($) => $.run_confirm.nothing_status);
    }
    // Single trigger → name the assignee (bolded), resolved from the preview's
    // runnable agent (squad leader for squads). Batch → count.
    if (preview.triggers.length === 1) {
      if (isSquad) {
        const name = d.assigneeName ?? getActorName("squad", d.assigneeId ?? "");
        return boldName(t(($) => $.run_confirm.will_start_named_squad, { name: `${NAME_FENCE}${name}${NAME_FENCE}` }));
      }
      const name = d.assigneeName ?? getActorName("agent", preview.triggers[0]!.agent_id);
      return boldName(t(($) => $.run_confirm.will_start_named, { name: `${NAME_FENCE}${name}${NAME_FENCE}` }));
    }
    return isSquad
      ? t(($) => $.run_confirm.will_start_squad, { count: preview.totalCount })
      : t(($) => $.run_confirm.will_start, { count: preview.totalCount });
  })();

  return (
    <Dialog open onOpenChange={(v) => { if (!v && !submitting) onClose(); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {mode === "assign" ? t(($) => $.run_confirm.title_assign) : t(($) => $.run_confirm.title_status)}
          </DialogTitle>
          <DialogDescription>
            {loading ? (
              <span className="flex items-center gap-1.5 text-muted-foreground">
                <Spinner className="size-3.5" />
                {t(($) => $.run_confirm.checking)}
              </span>
            ) : (
              headline
            )}
          </DialogDescription>
        </DialogHeader>

        {/* Assign mode keeps the note box mounted while the preview is in flight
            (disabled), so the dialog opens at its resolved height instead of
            growing when the predicate lands. Parked (no run) is the only case
            without a note, and it can't be a Backlog assign (those skip this
            modal), so it is rare. */}
        {mode === "assign" && (loading || canNote) ? (
          <div className="grid gap-1.5">
            <label className="text-sm font-medium" htmlFor="handoff-note">
              {t(($) => $.run_confirm.note_label)}
            </label>
            <Textarea
              id="handoff-note"
              value={note}
              maxLength={MAX_HANDOFF_NOTE}
              disabled={loading || noteDisabled || submitting}
              placeholder={t(($) => $.run_confirm.note_placeholder)}
              onChange={(e) => setNote(e.target.value)}
              rows={3}
            />
            {!loading && noteDisabled ? (
              <p className="text-xs text-muted-foreground">{t(($) => $.run_confirm.note_unsupported)}</p>
            ) : null}
          </div>
        ) : null}

        <DialogFooter>
          {loading ? (
            <Button type="button" disabled>
              <Spinner className="size-4" />
            </Button>
          ) : willStart ? (
            <>
              <Button type="button" variant="outline" disabled={submitting} onClick={() => submit(true)}>
                {pendingAction === "suppress" ? <Spinner className="size-4" /> : t(($) => $.run_confirm.dont_start)}
              </Button>
              <Button type="button" disabled={submitting} onClick={() => submit(false)}>
                {pendingAction === "go" ? <Spinner className="size-4" /> : t(($) => $.run_confirm.start)}
              </Button>
            </>
          ) : (
            <Button type="button" disabled={submitting} onClick={() => submit(false)}>
              {pendingAction === "go" ? <Spinner className="size-4" /> : t(($) => $.run_confirm.apply)}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
