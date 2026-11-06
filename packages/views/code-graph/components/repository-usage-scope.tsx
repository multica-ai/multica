"use client";

import { useState } from "react";
import type { ReactNode } from "react";
import type { CodeGraphRepository } from "@multica/core/types";
import { useCodeGraphScope } from "@multica/core/workspace/codegraph";
import { Bot, FolderOpen, Users } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { useGraphAssignmentWriter } from "./assignment-editor";
import { RepositoryAssignmentsDialog } from "./repository-assignments-dialog";
import { ProjectAssignmentsDialog } from "./project-assignments-dialog";
import { useT } from "../../i18n";

function ScopeSection({
  icon,
  title,
  description,
  count,
  action,
  children,
}: {
  icon: ReactNode;
  title: string;
  description: string;
  count?: number;
  action: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="flex min-w-0 flex-col rounded-lg border p-4">
      <div className="flex flex-col gap-3">
        <div className="min-w-0 flex-1">
          <h3 className="flex items-center gap-2 text-body font-medium">
            {icon}
            {title}
            {count !== undefined && <Badge variant="secondary">{count}</Badge>}
          </h3>
          <p className="mt-1 text-caption text-muted-foreground">
            {description}
          </p>
        </div>
      </div>
      <div className="mt-3 flex-1">{children}</div>
      {action && <div className="mt-4">{action}</div>}
    </section>
  );
}

export function RepositoryUsageScope({
  wsId,
  repo,
}: {
  wsId: string;
  repo: CodeGraphRepository;
}) {
  const { t } = useT("layout");
  const writer = useGraphAssignmentWriter(wsId);
  const scope = useCodeGraphScope(wsId);
  const [editing, setEditing] = useState<"agents" | "projects" | null>(null);
  const agents = repo.agents ?? [];
  const projects = repo.projects ?? [];
  const shared = repo.is_workspace_shared === true;
  const empty = !shared && !agents.length && !projects.length;
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-heading font-semibold">
          {t(($) => $.code_graph.scope.setup_title)}
        </h2>
        <p className="mt-1 text-body text-muted-foreground">
          {t(($) => $.code_graph.scope.setup_help)}
        </p>
        {empty && (
          <p className="mt-2 text-caption text-muted-foreground">
            {t(($) => $.code_graph.scope.unassigned_help)}
          </p>
        )}
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        <ScopeSection
          icon={<Bot className="size-4 shrink-0" aria-hidden="true" />}
          title={t(($) => $.code_graph.scope.selected_agents)}
          description={t(($) => $.code_graph.scope.agent_help)}
          count={agents.length}
          action={
            <Button
              variant="outline"
              size="sm"
              onClick={() => setEditing("agents")}
            >
              {t(($) => $.code_graph.scope.choose_agents)}
            </Button>
          }
        >
          {agents.length ? (
            <ul className="flex flex-wrap gap-2">
              {agents.map((agent) => (
                <li key={agent.id} className="min-w-0 max-w-full">
                  <Badge
                    variant="outline"
                    className="max-w-full whitespace-normal break-words"
                  >
                    {agent.name}
                  </Badge>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.code_graph.scope.no_agents)}
            </p>
          )}
        </ScopeSection>
        <ScopeSection
          icon={<FolderOpen className="size-4 shrink-0" aria-hidden="true" />}
          title={t(($) => $.code_graph.scope.projects)}
          description={t(($) => $.code_graph.scope.project_help)}
          count={projects.length}
          action={
            writer ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEditing("projects")}
              >
                {t(($) => $.code_graph.scope.choose_projects)}
              </Button>
            ) : null
          }
        >
          {projects.length ? (
            <ul className="flex flex-wrap gap-2">
              {projects.map((project) => (
                <li key={project.id} className="min-w-0 max-w-full">
                  <Badge
                    variant="outline"
                    className="max-w-full whitespace-normal break-words"
                  >
                    {project.name}
                  </Badge>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.code_graph.scope.no_projects)}
            </p>
          )}
        </ScopeSection>
        <ScopeSection
          icon={<Users className="size-4 shrink-0" aria-hidden="true" />}
          title={t(($) => $.code_graph.scope.workspace)}
          description={t(($) => $.code_graph.scope.workspace_help)}
          action={
            writer ? (
              <Button
                variant="outline"
                size="sm"
                disabled={scope.isPending}
                aria-busy={scope.isPending}
                onClick={() =>
                  scope.mutate({ repositoryId: repo.id, shared: !shared })
                }
              >
                {t(($) =>
                  shared
                    ? $.code_graph.scope.disable_sharing
                    : $.code_graph.scope.enable_sharing,
                )}
              </Button>
            ) : null
          }
        >
          <Badge variant={shared ? "secondary" : "outline"}>
            {t(($) =>
              shared
                ? $.code_graph.scope.sharing_on
                : $.code_graph.scope.sharing_off,
            )}
          </Badge>
        </ScopeSection>
      </div>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.code_graph.scope.union_help)}
      </p>
      <p className="text-caption text-muted-foreground">
        {t(($) => $.code_graph.scope.member_access)}
      </p>
      {!repo.active_build_id && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.code_graph.scope.awaiting_build)}
        </p>
      )}
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
      {editing === "agents" && (
        <RepositoryAssignmentsDialog
          wsId={wsId}
          repo={repo}
          onClose={() => setEditing(null)}
        />
      )}
      {editing === "projects" && (
        <ProjectAssignmentsDialog
          wsId={wsId}
          repo={repo}
          onClose={() => setEditing(null)}
        />
      )}
    </section>
  );
}
