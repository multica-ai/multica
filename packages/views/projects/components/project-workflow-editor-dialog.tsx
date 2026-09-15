"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import type { IssueWorkflowResponse } from "@multica/core/types";
import {
  effectiveIssueWorkflowOptions,
  workflowFromDefinition,
  workflowProblems,
  workflowToSpec,
  useApplyProjectWorkflow,
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

function snapshot(data: IssueWorkflowResponse) {
  const draft = workflowFromDefinition(data);
  return {
    draft,
    revision: data.workflow.revision,
    name: data.workflow.name,
    mode: data.mode,
    originalKeys: draft.statuses.map((s) => s.key),
  };
}

export function ProjectWorkflowEditorDialog({
  projectId,
  definition,
  statusKey,
  onClose,
}: {
  projectId: string;
  definition: IssueWorkflowResponse;
  statusKey?: string;
  onClose: () => void;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const query = useQuery(effectiveIssueWorkflowOptions(wsId, projectId, true));
  const apply = useApplyProjectWorkflow();
  const [editing, setEditing] = useState(() => snapshot(definition));
  const [showErrors, setShowErrors] = useState(false);
  const [confirmArchive, setConfirmArchive] = useState(false);
  const selectedStatus = editing.draft.statuses.find((s) => s.key === statusKey);
  const missingStatus = !!statusKey && !selectedStatus;
  const removed = editing.originalKeys.filter(
    (key) => !editing.draft.statuses.some((s) => s.key === key),
  );
  const save = () => {
    setShowErrors(true);
    if (missingStatus || workflowProblems(editing.draft).length) return;
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
          onClose();
          toast.success(t(($) => $.workflow.saved));
        },
      },
    );
  };
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !apply.isPending) onClose(); }}>
      <DialogContent className={`flex max-h-[90vh] flex-col overflow-hidden ${statusKey ? "sm:max-w-lg" : "sm:max-w-4xl"}`}>
        <DialogHeader>
          <DialogTitle>
            {statusKey
              ? t(($) => $.workflow.configure_status, { name: selectedStatus?.name ?? "" })
              : t(($) => $.workflow.edit)}
          </DialogTitle>
        </DialogHeader>
        <div className="min-h-0 space-y-4 overflow-y-auto overscroll-contain py-1">
          <WorkflowEditor
            value={editing.draft}
            statusKey={statusKey}
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
              {t(($) => $.workflow.customize_hint)}{" "}
              {t(($) => $.workflow.inherited_hint)}
            </p>
          )}
          {removed.length > 0 && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.workflow.removed_hint)}
            </p>
          )}
          {missingStatus && <p role="alert">{t(($) => $.workflow.load_error)}</p>}
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
                  setEditing(snapshot(result.data));
                  apply.reset();
                  setConfirmArchive(false);
                }}
              >
                {t(($) => $.workflow.reload)}
              </Button>
            </div>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" disabled={apply.isPending} onClick={onClose}>
            {t(($) => $.workflow.cancel)}
          </Button>
          <Button disabled={apply.isPending || apply.isError || missingStatus} onClick={save}>
            {apply.isPending
              ? t(($) => $.workflow.saving)
              : confirmArchive
                ? t(($) => $.workflow.confirm_removed)
                : t(($) => statusKey ? $.workflow_rules.save : $.workflow.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
