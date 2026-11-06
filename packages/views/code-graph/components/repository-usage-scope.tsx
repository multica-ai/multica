"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { projectListOptions } from "@multica/core/projects";
import type { CodeGraphRepository } from "@multica/core/types";
import { useCodeGraphScope } from "@multica/core/workspace/codegraph";
import { Button } from "@multica/ui/components/ui/button";
import {
  Field,
  FieldDescription,
  FieldLabel,
} from "@multica/ui/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useGraphAssignmentWriter } from "./assignment-editor";
import { useT } from "../../i18n";

export function RepositoryUsageScope({
  wsId,
  repo,
}: {
  wsId: string;
  repo: CodeGraphRepository;
}) {
  const { t } = useT("layout");
  const writer = useGraphAssignmentWriter(wsId);
  const projects = useQuery(projectListOptions(wsId));
  const scope = useCodeGraphScope(wsId);
  const [projectId, setProjectId] = useState("");
  const assigned = new Set(repo.projects?.map((project) => project.id));
  const candidates = (projects.data ?? []).filter(
    (project) => !assigned.has(project.id),
  );
  return (
    <section className="space-y-5">
      <p className="text-caption text-muted-foreground">
        {t(($) => $.code_graph.scope.member_access)}
      </p>
      <Field>
        <FieldLabel className="flex items-start gap-3">
          <input
            type="checkbox"
            checked={repo.is_workspace_shared === true}
            disabled={!writer || scope.isPending}
            onChange={(event) =>
              scope.mutate({
                repositoryId: repo.id,
                shared: event.target.checked,
              })
            }
          />
          {t(($) => $.code_graph.scope.workspace)}
        </FieldLabel>
        <FieldDescription>
          {t(($) => $.code_graph.scope.workspace_help)}
        </FieldDescription>
      </Field>
      <section className="space-y-3">
        <h3 className="text-body font-medium">
          {t(($) => $.code_graph.scope.projects)}
        </h3>
        <p className="text-caption text-muted-foreground">
          {t(($) => $.code_graph.scope.project_help)}
        </p>
        <ul className="divide-y">
          {(repo.projects ?? []).map((project) => (
            <li
              key={project.id}
              className="flex items-center justify-between gap-3 py-2"
            >
              <span className="min-w-0 break-words">{project.name}</span>
              {writer && (
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={scope.isPending}
                  onClick={() =>
                    scope.mutate({
                      repositoryId: repo.id,
                      projectId: project.id,
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
          <div className="flex items-center gap-2">
            <Select
              items={candidates.map((project) => ({
                value: project.id,
                label: project.title,
              }))}
              value={projectId}
              onValueChange={(value) => setProjectId(value ?? "")}
              disabled={scope.isPending || projects.isPending}
            >
              <SelectTrigger
                className="min-w-0 flex-1"
                aria-label={t(($) => $.code_graph.scope.projects)}
              >
                <SelectValue
                  placeholder={t(($) => $.code_graph.scope.select_project)}
                />
              </SelectTrigger>
              <SelectContent>
                {candidates.map((project) => (
                  <SelectItem key={project.id} value={project.id}>
                    {project.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              variant="outline"
              disabled={!projectId || scope.isPending}
              onClick={() =>
                scope.mutate(
                  { repositoryId: repo.id, projectId, assigned: true },
                  { onSuccess: () => setProjectId("") },
                )
              }
            >
              {t(($) => $.code_graph.assign)}
            </Button>
          </div>
        )}
        {projects.isError && (
          <p role="alert" className="text-caption text-destructive">
            {t(($) => $.code_graph.load_failed)}
          </p>
        )}
      </section>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.code_graph.scope.union_help)}
      </p>
      {scope.isPending && (
        <p role="status" className="text-caption">
          {t(($) => $.code_graph.scope.saving)}
        </p>
      )}
      {scope.error && (
        <p role="alert" className="text-caption text-destructive">
          {scope.error.message}
        </p>
      )}
    </section>
  );
}
