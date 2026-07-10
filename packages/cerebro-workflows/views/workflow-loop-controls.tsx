"use client";

// FIR-2283 followup point 4 — the loop controls (Loop on/off, Watch an issue,
// Activate on this issue, live loop-state + pending human checks) used to sit
// inline in the Issue-workflow editor form, where they made no sense next to
// the recipe fields. They now live on the run-history page, reached from its
// three-dot menu. "View run history" is intentionally dropped here — you are
// already on that page. Extracted verbatim from the old ControlStrip so the
// behaviour is unchanged; only its home and its enable/disable path (now the
// dedicated toggle endpoint instead of a full recipe re-save) differ.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import { IssuePickerModal } from "@multica/views/issues/components";
import type { Issue } from "@multica/core/types";
import {
  cerebroLoopStateOptions,
  cerebroWorkflowDetailOptions,
  cerebroWorkflowsKeys,
  toggleWorkflow,
  useActivateWorkflowMutation,
  useApproveHumanCheckMutation,
} from "../core";

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label className="text-xs">{label}</Label>
      {children}
    </div>
  );
}

export function WorkflowLoopControls({
  workflowId,
  wsId,
}: {
  workflowId: string;
  wsId: string;
}) {
  const queryClient = useQueryClient();
  const detail = useQuery({
    ...cerebroWorkflowDetailOptions(wsId, workflowId),
    enabled: !!wsId && !!workflowId,
  });
  const loopEnabled = detail.data?.enabled ?? false;

  const toggle = useMutation({
    mutationFn: (next: boolean) => toggleWorkflow(workflowId, next),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: cerebroWorkflowsKeys.all(wsId) });
    },
  });

  const [watchIssueId, setWatchIssueId] = useState("");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [watchIssue, setWatchIssue] = useState<Issue | null>(null);

  const loopState = useQuery({
    ...cerebroLoopStateOptions(wsId, workflowId, watchIssueId),
  });
  const approve = useApproveHumanCheckMutation(wsId, workflowId, watchIssueId);
  // Per-issue workflow activation. Reuses the same watch-issue selection: pick
  // an issue, then either just watch it or activate this recipe on it — the
  // recipe stays a reusable template; activating compiles an independent set
  // of rules for THIS issue alone.
  const activate = useActivateWorkflowMutation(wsId, watchIssueId);

  return (
    <div className="flex flex-col gap-3 rounded-md border bg-muted/30 p-3">
      <div className="flex flex-wrap items-end gap-3">
        <Field label="Loop">
          <Switch
            checked={loopEnabled}
            disabled={toggle.isPending || detail.isLoading}
            onCheckedChange={(v) => toggle.mutate(v === true)}
          />
        </Field>
        <Field label="Watch an issue">
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="w-56 justify-start font-normal"
            onClick={() => setPickerOpen(true)}
          >
            <span className="truncate">
              {watchIssue ? (
                <>
                  <span className="text-muted-foreground">{watchIssue.identifier}</span>{" "}
                  {watchIssue.title}
                </>
              ) : (
                <span className="text-muted-foreground">Search for an issue…</span>
              )}
            </span>
          </Button>
        </Field>
        <IssuePickerModal
          open={pickerOpen}
          onOpenChange={setPickerOpen}
          title="Watch an issue"
          description="Search by title and pick the issue to watch or activate this workflow on."
          excludeIds={[]}
          onSelect={(issue) => {
            setWatchIssue(issue);
            setWatchIssueId(issue.id);
          }}
        />
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!watchIssueId || activate.isPending}
          onClick={() => activate.mutate(workflowId)}
        >
          {activate.isPending ? "Activating…" : "Activate on this issue"}
        </Button>
      </div>
      {activate.isSuccess && (
        <p className="text-xs text-green-600 dark:text-green-400">
          Activated on this issue — it now runs this recipe independently of the project-wide
          compile.
        </p>
      )}
      {activate.isError && (
        <p className="text-xs text-destructive">
          {activate.error instanceof Error ? activate.error.message : "Could not activate"}
        </p>
      )}

      {watchIssueId && loopState.data && (
        <div className="flex flex-col gap-2 text-xs">
          <p className="text-muted-foreground">
            {loopState.data.stopped ? (
              <span className="font-medium text-amber-600 dark:text-amber-400">
                Paused · escalated to owner{loopState.data.stop_reason ? ` — ${loopState.data.stop_reason}` : ""}
              </span>
            ) : (
              <>
                Round {loopState.data.round}
                {loopState.data.max_iterations ? ` of ${loopState.data.max_iterations}` : ""}
              </>
            )}
          </p>
          {loopState.data.pending_human_checks.map((check) => (
            <div
              key={check.check_id}
              className="flex items-center justify-between gap-2 rounded-md border bg-background p-2"
            >
              <span className="min-w-0 truncate">Waiting on you: {check.prompt}</span>
              <div className="flex shrink-0 gap-1">
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => approve.mutate({ checkId: check.check_id, approved: true })}
                >
                  Approve
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() =>
                    approve.mutate({
                      checkId: check.check_id,
                      approved: false,
                      note: "Rejected from the loop controls",
                    })
                  }
                >
                  Reject
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
