"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Pencil, ArrowRight } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  effectiveIssueWorkflowOptions,
  workflowFromDefinition,
  workflowProblems,
  workflowToSpec,
  useApplyProjectWorkflow,
  type WorkflowDraft,
} from "@multica/core/issue-workflows";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { WorkflowEditor } from "./workflow-editor";

export function ProjectWorkflowSection({
  projectId,
  canEdit,
}: {
  projectId: string;
  canEdit: boolean;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const query = useQuery(effectiveIssueWorkflowOptions(wsId, projectId, true));
  const apply = useApplyProjectWorkflow();
  const { getActorName } = useActorName();
  const [editing, setEditing] = useState<{
    draft: WorkflowDraft;
    revision: number;
    name: string;
    mode: string;
    originalKeys: string[];
  } | null>(null);
  const [showErrors, setShowErrors] = useState(false);
  const [confirmArchive, setConfirmArchive] = useState(false);
  const open = () => {
    if (!query.data?.workflow.id) return;
    const draft = workflowFromDefinition(query.data);
    setEditing({
      draft,
      revision: query.data.workflow.revision,
      name: query.data.workflow.name,
      mode: query.data.mode,
      originalKeys: draft.statuses.map((s) => s.key),
    });
    setShowErrors(false);
    setConfirmArchive(false);
    apply.reset();
  };
  const removed =
    editing?.originalKeys.filter(
      (key) => !editing.draft.statuses.some((s) => s.key === key),
    ) ?? [];
  const save = () => {
    if (!editing) return;
    setShowErrors(true);
    if (workflowProblems(editing.draft).length) return;
    if (removed.length && !confirmArchive) {
      setConfirmArchive(true);
      return;
    }
    apply.mutate(
      {
        projectId,
        data: {
          mode: "custom",
          spec: workflowToSpec(editing.draft, editing.name),
          expected_revision: editing.revision,
          allow_archive: removed.length > 0,
        },
      },
      {
        onSuccess: () => {
          setEditing(null);
          toast.success(t(($) => $.workflow.saved));
        },
      },
    );
  };
  const active =
    query.data?.statuses
      .filter((s) => !s.archived_at)
      .sort((a, b) => a.position - b.position) ?? [];
  return (
    <section
      className="mx-auto w-full max-w-4xl space-y-6 p-6"
      aria-label={t(($) => $.workflow.title)}
    >
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="text-title font-semibold">
            {t(($) => $.workflow.title)}
          </h2>
          <p className="mt-1 text-body text-muted-foreground">
            {t(($) => $.workflow.editor_hint)}
          </p>
        </div>
        {canEdit && query.data?.workflow.id && (
          <Button variant="outline" onClick={open}>
            <Pencil className="size-3.5" />
            {t(($) => $.workflow.edit)}
          </Button>
        )}
      </header>
      {query.isLoading && (
        <p className="text-body text-muted-foreground">
          {t(($) => $.workflow_rules.loading)}
        </p>
      )}
      {(query.isError || (query.isSuccess && !query.data.workflow.id)) && (
        <div role="alert" className="space-y-2">
          <p>{t(($) => $.workflow.load_error)}</p>
          <Button variant="outline" onClick={() => void query.refetch()}>
            {t(($) => $.workflow.retry)}
          </Button>
        </div>
      )}
      {query.data?.mode === "default" && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.workflow_rules.mode_default)} ·{" "}
          {t(($) => $.workflow.inherited_hint)}
        </p>
      )}
      <div className="divide-y">
        {active.map((status) => (
          <div
            key={status.id}
            className="flex flex-wrap items-center justify-between gap-3 py-4"
          >
            <div className="flex min-w-0 items-center gap-3">
              <span
                className="size-2 shrink-0 rounded-full"
                style={{ backgroundColor: status.color }}
              />
              <span className="break-words text-body font-medium">
                {status.name}
              </span>
              {status.id === query.data?.workflow.initial_status_id && (
                <span className="text-caption text-muted-foreground">
                  {t(($) => $.workflow.start)}
                </span>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
              {status.entry_policy.executor.type !== "none"
                ? getActorName(
                    status.entry_policy.executor.type,
                    status.entry_policy.executor.id,
                  )
                : status.entry_policy.assignee.type === "keep"
                  ? t(($) => $.workflow.manual_hint)
                  : getActorName(
                      status.entry_policy.assignee.type === "human"
                        ? "member"
                        : status.entry_policy.assignee.type,
                      status.entry_policy.assignee.id,
                    )}
              {status.entry_policy.next_status_key && (
                <>
                  <ArrowRight className="size-3" />
                  {active.find(
                    (s) => s.spec_key === status.entry_policy.next_status_key,
                  )?.name ?? t(($) => $.workflow.no_next)}
                </>
              )}
            </div>
          </div>
        ))}
      </div>
      <Dialog
        open={!!editing}
        onOpenChange={(isOpen) => {
          if (!isOpen && !apply.isPending) setEditing(null);
        }}
      >
        <DialogContent className="flex max-h-[90vh] flex-col overflow-hidden sm:max-w-4xl">
          <DialogHeader>
            <DialogTitle>{t(($) => $.workflow.edit)}</DialogTitle>
          </DialogHeader>
          {editing && (
            <div className="min-h-0 space-y-4 overflow-y-auto py-3">
              <WorkflowEditor
                value={editing.draft}
                onChange={(draft) => {
                  setEditing({ ...editing, draft });
                  setConfirmArchive(false);
                }}
                showErrors={showErrors}
                disabled={apply.isPending}
              />
              <p className="text-caption text-muted-foreground">
                {t(($) => $.workflow.apply_hint)}
              </p>
              {editing.mode === "default" && (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.workflow.inherited_hint)}
                </p>
              )}
              {removed.length > 0 && (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.workflow.removed_hint)}
                </p>
              )}
              {apply.isError && (
                <div role="alert" className="space-y-2">
                  <p className="text-caption text-destructive">
                    {t(($) => $.workflow.save_error)}
                  </p>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={async () => {
                      const result = await query.refetch();
                      if (!result.data || result.isError) return;
                      const draft = workflowFromDefinition(result.data);
                      setEditing({
                        draft,
                        revision: result.data.workflow.revision,
                        name: result.data.workflow.name,
                        mode: result.data.mode,
                        originalKeys: draft.statuses.map((s) => s.key),
                      });
                      apply.reset();
                      setConfirmArchive(false);
                    }}
                  >
                    {t(($) => $.workflow.reload)}
                  </Button>
                </div>
              )}
            </div>
          )}
          <DialogFooter>
            <Button
              variant="ghost"
              disabled={apply.isPending}
              onClick={() => setEditing(null)}
            >
              {t(($) => $.workflow.cancel)}
            </Button>
            <Button disabled={apply.isPending || apply.isError} onClick={save}>
              {apply.isPending
                ? t(($) => $.workflow.saving)
                : confirmArchive
                  ? t(($) => $.workflow.confirm_removed)
                  : t(($) => $.workflow.save)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
