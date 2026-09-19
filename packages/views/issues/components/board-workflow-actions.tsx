"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { MoreHorizontal, Settings2 } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { useActorName } from "@multica/core/workspace/hooks";
import { effectiveIssueWorkflowOptions } from "@multica/core/issue-workflows";
import { Button } from "@multica/ui/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "../../common/actor-avatar";
import { DeferredPopup } from "../../common/deferred-popup";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import { ProjectWorkflowEditorDialog } from "../../projects/components/project-workflow-editor-dialog";
import { useT } from "../../i18n";

export function BoardWorkflowActions({ projectId, statusId }: {
  projectId: string;
  statusId: string;
}) {
  const wsId = useWorkspaceId();
  const { t } = useT("projects");
  const { role } = useCurrentMember(wsId);
  const { getActorName } = useActorName();
  const { data } = useQuery(effectiveIssueWorkflowOptions(wsId, projectId, true));
  const [editing, setEditing] = useState(false);
  // Historical columns belong to an older binding, not this project's editable definition.
  const status = data?.statuses.find((s) => s.id === statusId && !s.archived_at);
  if (!data?.workflow.id || !status) return null;
  const canEdit = role === "owner" || role === "admin";
  const executor = status.entry_policy.executor;
  const configureLabel = t(($) => $.workflow.configure_status, { name: status.name });
  const avatar = executor.type !== "none" ? (
    <ActorAvatar actorType={executor.type} actorId={executor.id} size="sm" profileLink={false} />
  ) : null;
  const executorLabel = executor.type !== "none"
    ? getActorName(executor.type, executor.id)
    : "";
  const menuTrigger = (
    <Button variant="ghost" size="icon-sm" className="rounded-full text-muted-foreground"
      aria-label={t(($) => $.workflow.column_options, { name: status.name })}>
      <MoreHorizontal className="size-3.5" />
    </Button>
  );
  return <>
    {avatar && (canEdit ? (
      <DeferredTooltip content={executorLabel} trigger={
        <Button variant="ghost" size="icon-sm" aria-label={`${configureLabel} · ${executorLabel}`}
          onClick={() => setEditing(true)}>{avatar}</Button>
      } />
    ) : <span title={executorLabel}>{avatar}</span>)}
    {canEdit && <DeferredPopup ariaHasPopup="menu" triggerRender={menuTrigger}>
      {(open, onOpenChange) => <DropdownMenu open={open} onOpenChange={onOpenChange}>
        <DropdownMenuTrigger render={menuTrigger} />
        <DropdownMenuContent align="end">
          <DropdownMenuItem onClick={() => setEditing(true)}>
            <Settings2 className="size-3.5" />
            {t(($) => executor.type === "none" ? $.workflow.configure_column : $.workflow.edit_column)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>}
    </DeferredPopup>}
    {editing && canEdit && <ProjectWorkflowEditorDialog projectId={projectId} definition={data}
      statusKey={status.spec_key} onClose={() => setEditing(false)} />}
  </>;
}
