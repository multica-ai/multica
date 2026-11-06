"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { codeGraphRepositoriesOptions } from "@multica/core/workspace/queries";
import { useCodeGraphScope } from "@multica/core/workspace/codegraph";
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useGraphAssignmentWriter } from "./assignment-editor";
import { useT } from "../../i18n";

export function ProjectCodeGraphs({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const { t } = useT("layout");
  const writer = useGraphAssignmentWriter(wsId);
  const graphs = useQuery(codeGraphRepositoriesOptions(wsId));
  const scope = useCodeGraphScope(wsId);
  const [repositoryId, setRepositoryId] = useState("");
  const repos = graphs.data?.items ?? [];
  const assigned = repos.filter(
    (repo) =>
      repo.is_workspace_shared ||
      repo.projects?.some((p) => p.id === projectId),
  );
  const candidates = repos.filter(
    (repo) => !repo.projects?.some((p) => p.id === projectId),
  );
  return (
    <section className="space-y-3 py-4">
      <h3 className="text-body font-medium">{t(($) => $.nav.code_graph)}</h3>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.code_graph.scope.project_help)}
      </p>
      <ul className="divide-y">
        {assigned.map((repo) => (
          <li
            key={repo.id}
            className="flex items-start justify-between gap-2 py-2"
          >
            <div className="min-w-0 break-words text-caption">
              {repo.repo_name} · {repo.branch}
              <p className="text-muted-foreground">
                {[
                  repo.is_workspace_shared
                    ? t(($) => $.code_graph.scope.source_workspace)
                    : "",
                  repo.projects?.some((p) => p.id === projectId)
                    ? t(($) => $.code_graph.scope.source_project)
                    : "",
                ]
                  .filter(Boolean)
                  .join(" · ")}
              </p>
              {!repo.active_build_id && (
                <p className="text-muted-foreground">
                  {t(($) => $.code_graph.scope.awaiting_build)}
                </p>
              )}
            </div>
            {writer && repo.projects?.some((p) => p.id === projectId) && (
              <Button
                variant="ghost"
                size="xs"
                disabled={scope.isPending}
                onClick={() =>
                  scope.mutate({
                    repositoryId: repo.id,
                    projectId,
                    assigned: false,
                  })
                }
              >
                {t(($) => $.code_graph.unassign)}
              </Button>
            )}
          </li>
        ))}
      </ul>
      {writer && (
        <div className="flex flex-wrap items-center gap-2">
          <Select
            items={candidates.map((repo) => ({
              value: repo.id,
              label: `${repo.repo_name} · ${repo.branch}`,
            }))}
            value={repositoryId}
            onValueChange={(value) => setRepositoryId(value ?? "")}
            disabled={scope.isPending || graphs.isPending}
          >
            <SelectTrigger
              className="min-w-0 flex-1"
              aria-label={t(($) => $.code_graph.scope.add_graph)}
            >
              <SelectValue
                placeholder={t(($) => $.code_graph.scope.add_graph)}
              />
            </SelectTrigger>
            <SelectContent>
              {candidates.map((repo) => (
                <SelectItem value={repo.id} key={repo.id}>
                  {repo.repo_name} · {repo.branch}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            size="sm"
            variant="outline"
            disabled={!repositoryId || scope.isPending}
            onClick={() =>
              scope.mutate(
                { repositoryId, projectId, assigned: true },
                { onSuccess: () => setRepositoryId("") },
              )
            }
          >
            {t(($) => $.code_graph.assign)}
          </Button>
        </div>
      )}
      {(graphs.isError || scope.isError) && (
        <p role="alert" className="text-caption text-destructive">
          {scope.error?.message ?? t(($) => $.code_graph.load_failed)}
        </p>
      )}
    </section>
  );
}
