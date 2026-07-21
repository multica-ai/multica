"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
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
import type { IssueAssigneeType, UpdateIssueRequest } from "@multica/core/types";
import { useUpdateIssue, useBatchUpdateIssues } from "@multica/core/issues/mutations";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { runtimeListOptions, readRuntimeCliVersion, handoffSupported } from "@multica/core/runtimes";
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
  // Assign is the only mode: agent/squad assignment is the sole issue write that
  // needs the pre-trigger confirmation. Batch status changes apply directly now
  // (MUL-4155), so there is no "status" mode.
  mode?: "assign";
  assigneeType?: IssueAssigneeType;
  assigneeId?: string;
  assigneeName?: string;
}

/**
 * Assignment confirmation for issues that may start agent runs.
 *
 * The rule is "dialog = you are confirming an assignment", NOT "you are
 * confirming N runs" (MUL-5010). It therefore does no pre-flight prediction:
 * opening it fires no request, so the note box and buttons are usable on the
 * first frame. Previously it called POST /api/issues/preview-trigger on open
 * and blocked the whole dialog behind a "检查中…" spinner; because that query is
 * keyed per issue id with staleTime 0, every new issue was a guaranteed cache
 * miss and the wait was unavoidable.
 *
 * Completion is silent: the assignee change and any run it starts surface
 * through the issue's normal assignee / run-status updates, so the confirm adds
 * no result toast. Whether a run starts stays the server's existing decision at
 * write time. Dismissing the dialog (X / Esc / click-outside) cancels without
 * any write. Shared by single assign (1 id) and batch assign (N ids).
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

  const [note, setNote] = useState("");
  // Which footer action is in flight, so only the clicked button shows a
  // spinner (the request runs an agent on the server for note assigns, so it is
  // not instant — the disabled-only state read as frozen).
  const [pendingAction, setPendingAction] = useState<"go" | "suppress" | null>(null);
  const submitting = pendingAction !== null;

  const updateIssue = useUpdateIssue();
  const batchUpdate = useBatchUpdateIssues();

  // Handoff-support verdict, resolved entirely from warm client caches
  // (useWorkspacePresencePrefetch keeps agents / squads / runtimes hot), so the
  // note box settles on the first frame with no round-trip — the same shape as
  // the quick-create version gate. An agent assignee targets its own runtime; a
  // squad targets its leader's, which the squad list gives us directly, so both
  // are knowable locally. `null` means "cannot tell" (assignee not in cache
  // yet, or no runtime bound) and leaves the box enabled: the note is a soft
  // gate, and a spurious warning is worse than a note an old daemon drops.
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: !!wsId });
  const { data: runtimes = [] } = useQuery({ ...runtimeListOptions(wsId), enabled: !!wsId });
  const { data: squads = [] } = useQuery({ ...squadListOptions(wsId), enabled: !!wsId });
  const localHandoff = useMemo<boolean | null>(() => {
    if (!d.assigneeId) return null;
    let agentId: string | undefined;
    if (d.assigneeType === "agent") {
      agentId = d.assigneeId;
    } else if (d.assigneeType === "squad") {
      // A squad run is executed by its leader, so the leader's runtime is the
      // one that has to render the note.
      agentId = squads.find((s) => s.id === d.assigneeId)?.leader_id;
    }
    if (!agentId) return null;
    const agent = agents.find((a) => a.id === agentId);
    if (!agent?.runtime_id) return null;
    const runtime = runtimes.find((r) => r.id === agent.runtime_id);
    if (!runtime) return null;
    return handoffSupported(readRuntimeCliVersion(runtime.metadata));
  }, [d.assigneeType, d.assigneeId, agents, runtimes, squads]);

  // Soft gate: an old runtime can't render the note. Disable the box but let
  // the assignment proceed (MUL-3375 §6.3).
  const noteDisabled = localHandoff === false;

  const applyTo = (extra: Partial<UpdateIssueRequest>) => {
    const base: UpdateIssueRequest = {
      assignee_type: d.assigneeType ?? null,
      assignee_id: d.assigneeId ?? null,
    };
    return { ...base, ...extra };
  };

  // The copy names whoever the issue is handed to; for a squad that is the
  // squad itself, since its leader deciding who works is an internal detail.
  const assigneeName =
    d.assigneeName ??
    getActorName(d.assigneeType === "squad" ? "squad" : "agent", d.assigneeId ?? "");

  const submit = async (suppressRun: boolean) => {
    if (issueIds.length === 0 || submitting) return;
    setPendingAction(suppressRun ? "suppress" : "go");
    const payload = applyTo({
      ...(suppressRun ? { suppress_run: true } : {}),
      ...(!suppressRun && !noteDisabled && note.trim() ? { handoff_note: note.trim() } : {}),
    });
    try {
      // Completion is silent, exactly as before: the assignee and any run show
      // up through the issue's normal assignee / run-status updates, so there is
      // no result toast to add here. Whether a run started is the server's
      // existing decision at write time, not something this dialog reports.
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

  // States the action, not a prediction: the assignment is certain, the run is
  // conditional, so the copy names no run count.
  const headline: ReactNode = boldName(
    issueIds.length > 1
      ? t(($) => $.run_confirm.assign_batch, {
          name: `${NAME_FENCE}${assigneeName}${NAME_FENCE}`,
          count: issueIds.length,
        })
      : t(($) => $.run_confirm.assign_single, {
          name: `${NAME_FENCE}${assigneeName}${NAME_FENCE}`,
        }),
  );

  return (
    <Dialog open onOpenChange={(v) => { if (!v && !submitting) onClose(); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(($) => $.run_confirm.title_assign)}</DialogTitle>
          <DialogDescription>{headline}</DialogDescription>
        </DialogHeader>

        {/* Always mounted and always usable on the first frame — nothing about
            this box depends on a server answer. */}
        <div className="grid gap-1.5">
          <label className="text-sm font-medium" htmlFor="handoff-note">
            {t(($) => $.run_confirm.note_label)}
          </label>
          <Textarea
            id="handoff-note"
            value={note}
            maxLength={MAX_HANDOFF_NOTE}
            disabled={submitting || noteDisabled}
            placeholder={t(($) => $.run_confirm.note_placeholder)}
            onChange={(e) => setNote(e.target.value)}
            rows={3}
          />
          {noteDisabled ? (
            <p className="text-xs text-muted-foreground">{t(($) => $.run_confirm.note_unsupported)}</p>
          ) : null}
        </div>

        {/* The only spinner left is on the button the user just pressed, and it
            reflects the write in flight — never a pre-flight check. */}
        <DialogFooter>
          <Button type="button" variant="outline" disabled={submitting} onClick={() => submit(true)}>
            {pendingAction === "suppress" ? <Spinner className="size-4" /> : t(($) => $.run_confirm.dont_start)}
          </Button>
          <Button type="button" disabled={submitting} onClick={() => submit(false)}>
            {pendingAction === "go" ? <Spinner className="size-4" /> : t(($) => $.run_confirm.confirm_assign)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
