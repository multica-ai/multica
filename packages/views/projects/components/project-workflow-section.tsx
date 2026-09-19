"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Pencil } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { effectiveIssueWorkflowOptions } from "@multica/core/issue-workflows";
import { Button } from "@multica/ui/components/ui/button";
import { ProjectWorkflowEditorDialog } from "./project-workflow-editor-dialog";
import { useT } from "../../i18n";

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
  const { getActorName } = useActorName();
  const [editing, setEditing] = useState<"custom" | "default" | null>(null);
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
          <div className="flex gap-2">
          {query.data.mode === "custom" && <Button variant="outline" onClick={() => setEditing("default")}>{t(($) => $.workflow.use_default)}</Button>}
          <Button variant="outline" onClick={() => setEditing("custom")}>
            <Pencil className="size-3.5" />
            {t(($) => $.workflow.edit)}
          </Button></div>
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
                : t(($) => $.workflow_rules.no_automatic_run)}
            </div>
          </div>
        ))}
      </div>
      {editing && canEdit && query.data && (
        <ProjectWorkflowEditorDialog projectId={projectId} definition={query.data} mode={editing} onClose={() => setEditing(null)} />
      )}
    </section>
  );
}
