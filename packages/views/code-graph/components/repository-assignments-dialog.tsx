"use client";
import { useAuthStore } from "@multica/core/auth";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { CodeGraphRepository } from "@multica/core/types";
import {
  agentSupportsCodeGraph,
  canManageCodeGraphAgent,
} from "@multica/core/workspace/codegraph";
import {
  agentListOptions,
  codeGraphRepositoryOptions,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import {
  AssignmentEditor,
  useGraphAssignmentWriter,
} from "./assignment-editor";
import { useT } from "../../i18n";

export function RepositoryAssignmentsDialog({
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
  const userId = useAuthStore((state) => state.user?.id);
  const details = useQuery(codeGraphRepositoryOptions(wsId, repo.id));
  const agents = useQuery(agentListOptions(wsId));
  const runtimes = useQuery(runtimeListOptions(wsId));
  const ready = details.isSuccess && agents.isSuccess && runtimes.isSuccess;
  const failed = details.isError || agents.isError || runtimes.isError;
  const assigned = new Set(details.data?.agents?.map((agent) => agent.id));
  const options = (agents.data ?? [])
    .filter((agent) => !agent.archived_at)
    .map((agent) => ({
      id: agent.id,
      label: agent.name,
      assigned: assigned.has(agent.id),
      editable: canManageCodeGraphAgent(agent, writer, userId),
      supported: agentSupportsCodeGraph(agent, runtimes.data ?? []),
    }));
  // Binding names may remain visible even when this client cannot edit their configuration.
  for (const agent of details.data?.agents ?? []) {
    if (!options.some((option) => option.id === agent.id))
      options.push({
        id: agent.id,
        label: agent.name,
        assigned: true,
        editable: false,
        supported: false,
      });
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.code_graph.assign_title)}</DialogTitle>
          <DialogDescription>{repo.repo_name}</DialogDescription>
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
                void details.refetch();
                void agents.refetch();
                void runtimes.refetch();
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
            toChange={(agentId, selected) => ({
              repositoryId: repo.id,
              agentId,
              assigned: selected,
            })}
            onDone={onClose}
            onBusyChange={setBusy}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
