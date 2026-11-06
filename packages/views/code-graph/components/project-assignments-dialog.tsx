"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "@multica/core/projects";
import { codeGraphRepositoryOptions } from "@multica/core/workspace/queries";
import type { CodeGraphRepository } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  AssignmentEditor,
  useGraphAssignmentWriter,
} from "./assignment-editor";
import { useT } from "../../i18n";

export function ProjectAssignmentsDialog({
  wsId,
  repo,
  onClose,
}: {
  wsId: string;
  repo: CodeGraphRepository;
  onClose: () => void;
}) {
  const { t } = useT("layout");
  const [busy, setBusy] = useState(false);
  const writer = useGraphAssignmentWriter(wsId);
  const projects = useQuery(projectListOptions(wsId));
  const details = useQuery(codeGraphRepositoryOptions(wsId, repo.id));
  const ready = projects.isSuccess && details.isSuccess;
  const failed = projects.isError || details.isError;
  const assigned = new Set(
    details.data?.projects?.map((project) => project.id),
  );
  const options = (projects.data ?? []).map((project) => ({
    id: project.id,
    label: project.title,
    assigned: assigned.has(project.id),
    editable: writer,
    supported: true,
  }));
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            {t(($) => $.code_graph.scope.choose_projects)}
          </DialogTitle>
          <DialogDescription>
            {repo.repo_name} · {t(($) => $.code_graph.scope.project_help)}
          </DialogDescription>
        </DialogHeader>
        {!ready && !failed && (
          <p role="status">{t(($) => $.code_graph.loading)}</p>
        )}
        {failed && (
          <div role="alert">
            {t(($) => $.code_graph.load_failed)}
            <Button
              variant="outline"
              onClick={() => {
                void projects.refetch();
                void details.refetch();
              }}
            >
              {t(($) => $.code_graph.retry)}
            </Button>
          </div>
        )}
        {ready && (
          <AssignmentEditor
            wsId={wsId}
            options={options}
            toChange={(targetId, assigned) => ({
              repositoryId: repo.id,
              target: "project",
              targetId,
              assigned,
            })}
            onDone={onClose}
            onBusyChange={setBusy}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
