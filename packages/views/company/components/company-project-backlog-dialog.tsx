"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import type { Project, Squad } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { issueKeys } from "@multica/core/issues/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { Plus } from "@multica/ui/icons";
import { useT } from "../../i18n";

/**
 * Shown once a project is assigned to a squad. It lists the project's backlog and
 * opens the new-issue form already pointed at the squad's leader. The SDLC workflow
 * starts when the leader takes the issue.
 */
export function CompanyProjectBacklogDialog({
  squad,
  project,
  onClose,
}: {
  squad: Squad | null;
  project: Project | null;
  onClose: () => void;
}) {
  return (
    <Dialog open={squad !== null && project !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="flex max-h-[min(88dvh,36rem)] flex-col gap-0 p-0 sm:max-w-md">
        {squad && project ? (
          <BacklogBody key={`${squad.id}:${project.id}`} squad={squad} project={project} onClose={onClose} />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function BacklogBody({ squad, project, onClose }: { squad: Squad; project: Project; onClose: () => void }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const leader = agents.find((agent) => agent.id === squad.leader_id);
  const { data: backlog = [], isLoading } = useQuery({
    queryKey: [...issueKeys.all(wsId), "project-backlog", project.id],
    queryFn: () => api.listIssues({ project_id: project.id, status: "backlog" }).then((res) => res.issues),
  });

  const createForLeader = () => {
    onClose();
    useModalStore.getState().open("create-issue", {
      project_id: project.id,
      assignee_type: "agent",
      assignee_id: squad.leader_id,
    });
  };

  return (
    <>
      <DialogHeader className="gap-1 border-b px-4 py-3 pr-12">
        <DialogTitle className="truncate text-heading">
          {t(($) => $.company.backlog_title, { project: project.title })}
        </DialogTitle>
        <p className="text-body text-muted-foreground">
          {t(($) => $.company.backlog_description, { name: leader?.name ?? squad.name })}
        </p>
      </DialogHeader>
      <ul className="flex min-h-0 flex-col gap-1 overflow-y-auto px-4 py-3" aria-label={t(($) => $.company.backlog_list)}>
        {!isLoading && backlog.length === 0 ? (
          <li className="py-6 text-center text-body text-muted-foreground">{t(($) => $.company.backlog_empty)}</li>
        ) : null}
        {backlog.map((issue) => (
          <li key={issue.id} className="flex items-center gap-2 rounded-md border px-3 py-2 text-body">
            <span className="shrink-0 text-caption text-muted-foreground">{issue.identifier}</span>
            <span className="truncate">{issue.title}</span>
          </li>
        ))}
      </ul>
      <DialogFooter className="flex-row justify-between gap-2 border-t px-4 py-3">
        <Button type="button" variant="ghost" size="sm" onClick={onClose}>
          {t(($) => $.company.backlog_later)}
        </Button>
        <Button type="button" size="sm" onClick={createForLeader}>
          <Plus className="size-4" />
          {t(($) => $.company.backlog_create)}
        </Button>
      </DialogFooter>
    </>
  );
}
