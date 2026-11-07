"use client";

import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { FolderMinus, Monitor, Moon, SquarePen, Sun } from "@multica/ui/icons";
import { Command as CommandPrimitive } from "cmdk";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { useModalStore } from "@multica/core/modals";
import { PROJECT_STATUS_CONFIG, PROJECT_STATUS_ORDER } from "@multica/core/projects/config";
import { useUpdateProject } from "@multica/core/projects/mutations";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Agent, ChatSession, Project } from "@multica/core/types";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { cn } from "@multica/ui/lib/utils";
import { runConfirmIntent } from "../../issues/actions/run-confirm-gate";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useProjectStatusLabels } from "../../projects/components/labels";
import { useT } from "../../i18n";
import { HighlightText } from "../highlight-text";
import { scoreRow, scoreText } from "../matching";
import { usePalette } from "../palette-context";
import { PaletteGroup, ROW_CLASS, ROW_ICON_CLASS, RowCheck } from "../rows";
import { ActorRow, AgentPresenceText, IssueResultRow } from "../result-rows";
import { useServerSearch } from "../use-server-search";
import type { DelegateActor } from "../types";
import { useDelegateCandidates } from "../delegate/use-delegate";

type ThemeValue = "light" | "dark" | "system";

export function ThemePage({ query }: { query: string }) {
  const { t } = useT("search");
  const palette = usePalette();
  const { theme, setTheme } = useTheme();
  const q = query.trim().toLowerCase();
  const options: { value: ThemeValue; label: string; icon: typeof Sun; keywords: string[] }[] = [
    { value: "light", label: t(($) => $.theme.light), icon: Sun, keywords: ["light", "bright", "浅色"] },
    { value: "dark", label: t(($) => $.theme.dark), icon: Moon, keywords: ["dark", "night", "深色"] },
    { value: "system", label: t(($) => $.theme.system), icon: Monitor, keywords: ["system", "auto", "系统"] },
  ];
  const visible = q ? options.filter((o) => scoreRow(o.label, o.keywords, q) > 0) : options;
  return (
    <PaletteGroup heading={t(($) => $.pages.theme)}>
      {visible.map((option) => (
        <CommandPrimitive.Item
          key={option.value}
          value={`theme:${option.value}`}
          onSelect={() => {
            palette.close();
            setTheme(option.value);
          }}
          className={ROW_CLASS}
        >
          <option.icon className={ROW_ICON_CLASS} />
          <span className="min-w-0 flex-1 truncate">
            <HighlightText text={option.label} query={query} />
          </span>
          {theme === option.value && <RowCheck label={t(($) => $.commands.current_theme_aria)} />}
        </CommandPrimitive.Item>
      ))}
    </PaletteGroup>
  );
}

export function ProjectStatusPage({ project, query }: { project: Project; query: string }) {
  const { t } = useT("search");
  const palette = usePalette();
  const labels = useProjectStatusLabels();
  const updateProject = useUpdateProject();
  const q = query.trim().toLowerCase();
  const visible = PROJECT_STATUS_ORDER.filter(
    (status) => !q || scoreRow(labels[status] ?? status, [status.replace(/_/g, " ")], q) > 0,
  );
  return (
    <PaletteGroup heading={t(($) => $.pages.project_status)}>
      {visible.map((status) => (
        <CommandPrimitive.Item
          key={status}
          value={`project-status:${status}`}
          onSelect={() => {
            palette.close();
            updateProject.mutateAsync({ id: project.id, status }).catch((err: unknown) =>
              toast.error(err instanceof Error && err.message ? err.message : t(($) => $.toast.update_failed)),
            );
          }}
          className={ROW_CLASS}
        >
          <span className="flex size-4 shrink-0 items-center justify-center">
            <span className={cn("size-2 rounded-full", PROJECT_STATUS_CONFIG[status].dotColor)} />
          </span>
          <span className="min-w-0 flex-1 truncate">
            <HighlightText text={labels[status] ?? status} query={query} />
          </span>
          {project.status === status && <RowCheck label={t(($) => $.pages.current)} />}
        </CommandPrimitive.Item>
      ))}
    </PaletteGroup>
  );
}

/** From an agent page: pick an existing issue and assign it to the agent. */
export function AgentAssignIssuePage({ agent, query }: { agent: Agent; query: string }) {
  const { t } = useT("search");
  const { t: tIssues } = useT("issues");
  const wsId = useWorkspaceId();
  const palette = usePalette();
  const queryClient = useQueryClient();
  const updateIssue = useUpdateIssue();
  const { entryOf } = useIssueStatuses(wsId);
  const openModal = useModalStore((s) => s.open);
  const { results, isLoading, stale } = useServerSearch(query, { projects: false });
  const newIssueLabel = t(($) => $.agent.new_issue, { name: agent.name });
  const q = query.trim().toLowerCase();

  const assign = async (issueId: string) => {
    palette.close();
    const updates = { assignee_type: "agent" as const, assignee_id: agent.id };
    try {
      const issue = await queryClient.fetchQuery(issueDetailOptions(wsId, issueId));
      // Same gate as every single-issue assignment: confirm before a run starts.
      const intent = runConfirmIntent(issue, updates, { entryOf });
      if (intent) {
        openModal("issue-run-confirm", intent);
        return;
      }
      await updateIssue.mutateAsync({ id: issueId, ...updates });
      toast.success(t(($) => $.toast.assigned, { identifier: issue.identifier, name: agent.name }));
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : tIssues(($) => $.detail.update_failed));
    }
  };

  return (
    <>
      {(!q || scoreText(newIssueLabel, q) > 0) && (
        <PaletteGroup heading={t(($) => $.groups.new)}>
          <CommandPrimitive.Item
            value="agent-new-issue"
            onSelect={() => {
              palette.close();
              useModalStore.getState().open("quick-create-issue", { agent_id: agent.id });
            }}
            className={ROW_CLASS}
          >
            <SquarePen className={ROW_ICON_CLASS} />
            <span className="min-w-0 flex-1 truncate">{newIssueLabel}</span>
          </CommandPrimitive.Item>
        </PaletteGroup>
      )}
      {!q && (
        <div className="px-4 py-3 text-caption text-muted-foreground">{t(($) => $.empty.type_to_find_issue)}</div>
      )}
      {results.issues.length > 0 && (
        <PaletteGroup heading={t(($) => $.groups.issues)}>
          {results.issues.map((issue) => (
            <IssueResultRow
              key={issue.id}
              issue={issue}
              query={results.query}
              disabled={stale}
              onSelect={() => void assign(issue.id)}
            />
          ))}
        </PaletteGroup>
      )}
      {!isLoading && !stale && q && results.issues.length === 0 && (
        <div className="py-8 text-center text-body text-muted-foreground">{t(($) => $.empty.no_results)}</div>
      )}
    </>
  );
}

export function ChatProjectPage({
  session,
  changeProject,
  query,
}: {
  session: ChatSession;
  changeProject: (projectId: string | null) => void;
  query: string;
}) {
  const { t } = useT("search");
  const wsId = useWorkspaceId();
  const palette = usePalette();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const current = session.project_id ?? null;
  const q = query.trim().toLowerCase();
  const visible = projects
    .filter((project) => project.status !== "cancelled" || project.id === current)
    .filter((project) => !q || scoreText(project.title, q) > 0);
  const noProjectLabel = t(($) => $.pages.no_project);
  const select = (projectId: string | null) => {
    palette.close();
    changeProject(projectId);
  };
  return (
    <PaletteGroup heading={t(($) => $.pages.project)}>
      {current && (!q || scoreText(noProjectLabel, q) > 0) && (
        <CommandPrimitive.Item value="chat-project:none" onSelect={() => select(null)} className={ROW_CLASS}>
          <FolderMinus className={ROW_ICON_CLASS} />
          <span className="min-w-0 flex-1 truncate text-muted-foreground">{noProjectLabel}</span>
        </CommandPrimitive.Item>
      )}
      {visible.map((project) => (
        <CommandPrimitive.Item
          key={project.id}
          value={`chat-project:${project.id}`}
          onSelect={() => select(project.id)}
          className={ROW_CLASS}
        >
          <ProjectIcon project={project} size="md" />
          <span className="min-w-0 flex-1 truncate">
            <HighlightText text={project.title} query={query} />
          </span>
          {current === project.id && <RowCheck label={t(($) => $.pages.current)} />}
        </CommandPrimitive.Item>
      ))}
    </PaletteGroup>
  );
}

export function DelegateTargetPage({
  query,
  onSubmit,
}: {
  query: string;
  onSubmit: (actor: DelegateActor) => void;
}) {
  const { t } = useT("search");
  const { t: tIssues } = useT("issues");
  const palette = usePalette();
  const { actors, agentEntries } = useDelegateCandidates(true);
  const q = query.trim().toLowerCase();
  const visible = q ? actors.filter((actor) => scoreText(actor.name, q) > 0) : actors;
  const agents = visible.filter((actor) => actor.type === "agent");
  const squads = visible.filter((actor) => actor.type === "squad");
  const entryOf = (id: string) => agentEntries.find((entry) => entry.agent.id === id) ?? null;
  const select = (actor: DelegateActor) => {
    palette.close();
    onSubmit(actor);
  };
  return (
    <>
      {agents.length > 0 && (
        <PaletteGroup heading={tIssues(($) => $.pickers.assignee.agents_group)}>
          {agents.map((actor) => {
            const entry = entryOf(actor.id);
            return (
              <ActorRow
                key={actor.id}
                value={`delegate-target:agent:${actor.id}`}
                actorType="agent"
                actorId={actor.id}
                name={actor.name}
                query={query}
                showStatusDot
                onSelect={() => select(actor)}
                detail={
                  entry ? (
                    <AgentPresenceText availability={entry.availability} runtimeLabel={entry.runtimeLabel} />
                  ) : undefined
                }
              />
            );
          })}
        </PaletteGroup>
      )}
      {squads.length > 0 && (
        <PaletteGroup heading={tIssues(($) => $.pickers.assignee.squads_group)}>
          {squads.map((actor) => (
            <ActorRow
              key={actor.id}
              value={`delegate-target:squad:${actor.id}`}
              actorType="squad"
              actorId={actor.id}
              name={actor.name}
              query={query}
              onSelect={() => select(actor)}
            />
          ))}
        </PaletteGroup>
      )}
      {visible.length === 0 && (
        <div className="py-8 text-center text-body text-muted-foreground">{t(($) => $.empty.no_results)}</div>
      )}
    </>
  );
}
