"use client";
import { useAuthStore } from "@multica/core/auth";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { codeGraphRepositoriesOptions } from "@multica/core/workspace/queries";
import { canManageCodeGraphAgent } from "@multica/core/workspace/codegraph";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  AssignmentEditor,
  useGraphAssignmentWriter,
} from "./assignment-editor";
import { useT } from "../../i18n";

export function AgentCodeGraphsTab({
  agent,
  runtime,
}: {
  agent: Agent;
  runtime: AgentRuntime | null | undefined;
}) {
  const wsId = useWorkspaceId();
  const { t } = useT("layout");
  const repos = useQuery({
    ...codeGraphRepositoriesOptions(wsId),
    enabled: Boolean(wsId),
  });
  const writer = useGraphAssignmentWriter(wsId);
  const userId = useAuthStore((state) => state.user?.id);
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const supported = runtime?.supports_code_graph === true;
  const editable = canManageCodeGraphAgent(agent, writer, userId);
  const options = (repos.data?.items ?? []).map((repo) => ({
    id: repo.id,
    label: `${repo.repo_name} · ${repo.branch}`,
    assigned: Boolean(repo.agents?.some((row) => row.id === agent.id)),
    editable,
    supported,
  }));
  const assigned = options.filter(
    (option) =>
      option.assigned ||
      repos.data?.items.find((repo) => repo.id === option.id)
        ?.is_workspace_shared,
  );
  return (
    <section className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <h2 className="text-heading font-semibold">
          {t(($) => $.nav.code_graph)}
        </h2>
        {repos.isSuccess && editable && (supported || assigned.length > 0) && (
          <Button variant="outline" onClick={() => setEditing(true)}>
            {t(($) => $.code_graph.assignment_manage)}
          </Button>
        )}
      </div>
      {!supported && (
        <p className="text-body text-muted-foreground">
          {t(($) => $.code_graph.assignment_external)}
        </p>
      )}
      {repos.isPending && <p role="status">{t(($) => $.code_graph.loading)}</p>}
      {repos.isError && (
        <div role="alert">
          {t(($) => $.code_graph.load_failed)}
          <Button variant="outline" onClick={() => void repos.refetch()}>
            {t(($) => $.code_graph.retry)}
          </Button>
        </div>
      )}
      {repos.isSuccess && !assigned.length && (
        <p className="text-body text-muted-foreground">
          {t(($) => $.code_graph.agent_graphs_empty)}
        </p>
      )}
      <ul className="divide-y rounded-lg border empty:hidden">
        {assigned.map((option) => (
          <li className="break-words p-3 text-body" key={option.id}>
            {option.label}
            <p className="text-caption text-muted-foreground">
              {[
                option.assigned
                  ? t(($) => $.code_graph.scope.source_agent)
                  : "",
                repos.data?.items.find((repo) => repo.id === option.id)
                  ?.is_workspace_shared
                  ? t(($) => $.code_graph.scope.source_workspace)
                  : "",
              ]
                .filter(Boolean)
                .join(" · ")}
            </p>
            {!repos.data?.items.find((repo) => repo.id === option.id)
              ?.active_build_id && (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.code_graph.scope.awaiting_build)}
              </p>
            )}
          </li>
        ))}
      </ul>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.code_graph.scope.project_help)}
      </p>
      <Dialog
        open={editing}
        onOpenChange={(open) => {
          if (!busy) setEditing(open);
        }}
      >
        <DialogContent
          aria-describedby={undefined}
          className="max-h-[85dvh] overflow-y-auto sm:max-w-xl"
        >
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.code_graph.assignment_manage)}
            </DialogTitle>
          </DialogHeader>
          {editing && (
            <AssignmentEditor
              wsId={wsId}
              options={options}
              toChange={(repositoryId, selected) => ({
                repositoryId,
                agentId: agent.id,
                assigned: selected,
              })}
              onDone={() => setEditing(false)}
              onBusyChange={setBusy}
            />
          )}
        </DialogContent>
      </Dialog>
    </section>
  );
}
