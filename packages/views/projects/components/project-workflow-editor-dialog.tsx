"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ApplyProjectWorkflowRequest, IssueWorkflowResponse } from "@multica/core/types";
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
  mode = "custom",
}: {
  mode?: "custom" | "default";
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
  const [preview, setPreview] = useState<IssueWorkflowResponse | null>(null);
  const [mapping, setMapping] = useState<Record<string, string>>({});
  const migration = preview?.plan?.migration;
  const blocked = (migration?.blocked_issue_ids.length ?? 0) > 0;
  const incomplete = migration?.rows.some((row) => row.required && !mapping[row.source_status_id]);
  const selectedStatus = editing.draft.statuses.find((s) => s.key === statusKey);
  const missingStatus = !!statusKey && !selectedStatus;
  const removed = editing.originalKeys.filter(
    (key) => !editing.draft.statuses.some((s) => s.key === key),
  );
  const request: ApplyProjectWorkflowRequest = {
    mode,
    ...(mode === "custom" ? { spec: workflowToSpec(editing.draft, editing.name) } : {}),
    expected_revision: editing.revision,
    allow_archive: removed.length > 0,
  };
  const finish = () => {
    onClose();
    toast.success(t(($) => $.workflow.saved));
  };
  const save = () => {
    setShowErrors(true);
    if (missingStatus || (mode === "custom" && workflowProblems(editing.draft).length)) return;
    apply.mutate(
      {
        projectId,
        data: {
          ...request,
          dry_run: !preview,
          status_mapping: mapping,
          confirm_migration: !!preview,
          migration_fingerprint: migration?.fingerprint,
        },
      },
      {
        onSuccess: (result) => {
          if (!preview) {
            if (!result.dry_run || !result.plan?.migration) return;
            if (result.plan.migration.issue_count === 0 && result.plan.migration.view_count === 0 && !result.plan.migration.rows.some((row) => row.required && !row.target_key)) {
              apply.mutate({ projectId, data: request }, { onSuccess: finish });
              return;
            }
            setPreview(result);
            setMapping(Object.fromEntries(result.plan.migration.rows.map((row) => [row.source_status_id, row.target_key])));
            return;
          }
          finish();
        },
      },
    );
  };
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !apply.isPending) onClose(); }}>
      <DialogContent className={`flex max-h-[90vh] flex-col overflow-hidden ${statusKey ? "sm:max-w-lg" : "sm:max-w-4xl"}`}>
        <DialogHeader>
          <DialogTitle>
            {mode === "default" ? t(($) => $.workflow.use_default) : statusKey
              ? t(($) => $.workflow.configure_status, { name: selectedStatus?.name ?? "" })
              : t(($) => $.workflow.edit)}
          </DialogTitle>
        </DialogHeader>
        <div className="min-h-0 space-y-4 overflow-y-auto overscroll-contain py-1">
          {mode === "custom" && !preview && <WorkflowEditor
            value={editing.draft}
            statusKey={statusKey}
            onChange={(draft) => {
              setEditing({ ...editing, draft });
              setPreview(null);
              setMapping({});
            }}
            showErrors={showErrors}
            disabled={apply.isPending}
          />}
          {migration && <div className="space-y-4">
            <p>{t(($) => $.workflow.migration_summary, { issues: migration.issue_count, views: migration.view_count })}</p>
            <div className="space-y-3">
              {migration.rows.filter((row) => row.required || row.target_key).map((row) => (
                <label key={row.source_status_id} className="grid grid-cols-2 items-center gap-3 text-body">
                  <span>{row.source_name} · {row.count}</span>
                  <select className="h-9 w-full rounded-md border bg-background px-2 focus-visible:outline-ring" value={mapping[row.source_status_id] ?? ""} disabled={apply.isPending} onChange={(event) => setMapping({ ...mapping, [row.source_status_id]: event.target.value })}>
                    <option value="">{t(($) => $.workflow.choose_target)}</option>
                    {preview.statuses.filter((s) => !s.archived_at).map((s) => <option key={s.spec_key} value={s.spec_key}>{s.name}</option>)}
                  </select>
                </label>
              ))}
            </div>
            {migration.rows.some((row) => { const target = preview.statuses.find((s) => s.spec_key === mapping[row.source_status_id]); return target && target.phase !== row.source_phase; }) && <p role="alert" className="text-caption text-destructive">{t(($) => $.workflow.phase_warning)}</p>}
            {blocked && <div role="alert"><p>{t(($) => $.workflow.active_work_blocked)}</p><ul className="text-caption">{migration.blocked_issue_ids.map((id) => <li key={id}>{id}</li>)}</ul></div>}
          </div>}
          <p className="text-caption text-muted-foreground">
            {t(($) => $.workflow.apply_hint)}
          </p>
          {editing.mode === "default" && mode === "custom" && (
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
                {apply.error instanceof Error ? apply.error.message : t(($) => $.workflow.save_error)}
              </p>
              <Button
                variant="outline"
                size="sm"
                onClick={async () => {
                  const result = await query.refetch();
                  if (!result.data || result.isError) return;
                  setEditing(snapshot(result.data));
                  apply.reset();
                  setPreview(null);
                  setMapping({});
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
          {preview && <Button variant="outline" disabled={apply.isPending} onClick={() => { setPreview(null); setMapping({}); apply.reset(); }}>{t(($) => $.workflow.back_to_edit)}</Button>}
          <Button disabled={apply.isPending || apply.isError || missingStatus || blocked || !!incomplete} onClick={save}>
            {apply.isPending
              ? t(($) => $.workflow.saving)
              : preview
                ? t(($) => $.workflow.confirm_migration)
                : t(($) => statusKey ? $.workflow_rules.save : $.workflow.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
