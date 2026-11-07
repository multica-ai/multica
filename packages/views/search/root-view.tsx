"use client";

import type { ReactNode } from "react";
import { Clock, Loader2, MessageSquare, Sparkles, SquarePen, UserPlus, Zap, type IconComponent } from "@multica/ui/icons";
import { Command as CommandPrimitive } from "cmdk";
import { useQuery } from "@tanstack/react-query";
import { inboxUnreadSummaryOptions, unreadWorkspaceIds } from "@multica/core/inbox/queries";
import { paths, useWorkspacePaths } from "@multica/core/paths";
import type { InboxWorkspaceUnread } from "@multica/core/types";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores";
import { ActorAvatar } from "../common/actor-avatar";
import { routeIconForPath } from "../layout/route-icon-components";
import { ProjectIcon } from "../projects/components/project-icon";
import { WorkspaceAvatar } from "../workspace/workspace-avatar";
import { useT } from "../i18n";
import { HighlightText } from "./highlight-text";
import { useIssueTargetWriter } from "./issue-target";
import { usePalette } from "./palette-context";
import { CommandGroupView, CommandRow, PaletteGroup, ROW_CLASS, ROW_ICON_CLASS, RowHint } from "./rows";
import { ActorRow, AgentPresenceText, IssueResultRow, ObjectRow, ProjectResultRow } from "./result-rows";
import { DelegateGroup } from "./delegate/delegate-group";
import type { DelegateActor, IssueTarget } from "./types";
import type { BestMatch, RootModel } from "./use-root-model";

const EMPTY_UNREAD_SUMMARY: InboxWorkspaceUnread[] = [];

// Group render order when scores tie. Lower first.
const TYPE_ORDER = [
  "context",
  "pages",
  "commands",
  "agents",
  "squads",
  "members",
  "projects",
  "issues",
  "autopilots",
  "chats",
  "skills",
  "workspaces",
] as const;

interface RankedGroup {
  key: string;
  type: (typeof TYPE_ORDER)[number];
  score: number;
  node: ReactNode;
}

export function RootView({
  model,
  rawQuery,
  onDelegate,
}: {
  model: RootModel;
  rawQuery: string;
  onDelegate: (actor: DelegateActor, prompt: string) => void;
}) {
  const { t } = useT("search");
  const palette = usePalette();
  const p = useWorkspacePaths();
  const { data: unreadSummary = EMPTY_UNREAD_SUMMARY } = useQuery(inboxUnreadSummaryOptions());
  const unreadWsIds = unreadWorkspaceIds(unreadSummary);
  const { server, partitioned, objects, text } = model;
  const query = text;

  if (!rawQuery.trim()) {
    return <EmptyState model={model} />;
  }

  const best = model.bestMatch;
  const isBest = (type: BestMatch["type"], id: string) => {
    if (!best || best.type !== type) return false;
    if (best.type === "agent") return best.entry.agent.id === id;
    if (best.type === "squad") return best.entry.squad.id === id;
    if (best.type === "member") return best.member.user_id === id;
    return best.project.id === id;
  };

  const groups: RankedGroup[] = [];
  for (const group of model.contextGroups) {
    groups.push({
      key: group.id,
      type: "context",
      // Commands on what is on screen beat an equally good global hit.
      score: group.score + 5,
      node: <CommandGroupView key={group.id} group={group} query={query} />,
    });
  }
  if (model.pages.length > 0) {
    groups.push({
      key: "pages",
      type: "pages",
      score: model.pages[0]!.score,
      node: (
        <PaletteGroup key="pages" heading={t(($) => $.groups.pages)}>
          {model.pages.map(({ item: page }) => {
            const PageIcon = routeIconForPath(p[page.key]());
            return (
              <CommandPrimitive.Item
                key={page.key}
                value={`page:${page.key}`}
                onSelect={() => palette.navigate(p[page.key]())}
                className={ROW_CLASS}
              >
                <PageIcon className={ROW_ICON_CLASS} />
                <span className="min-w-0 flex-1 truncate">
                  <HighlightText text={page.label} query={query} />
                </span>
              </CommandPrimitive.Item>
            );
          })}
        </PaletteGroup>
      ),
    });
  }
  if (model.commands.length > 0) {
    groups.push({
      key: "commands",
      type: "commands",
      score: model.commands[0]!.score,
      node: (
        <PaletteGroup key="commands" heading={t(($) => $.groups.commands)}>
          {model.commands.map(({ item }) => (
            <CommandRow key={item.id} command={item} query={query} />
          ))}
        </PaletteGroup>
      ),
    });
  }

  const agents = objects.agents.filter((hit) => !isBest("agent", hit.item.agent.id));
  if (agents.length > 0) {
    groups.push({
      key: "agents",
      type: "agents",
      score: agents[0]!.score,
      node: (
        <PaletteGroup key="agents" heading={t(($) => $.groups.agents)}>
          {agents.map(({ item: entry }) => (
            <ActorRow
              key={entry.agent.id}
              value={`agent:${entry.agent.id}`}
              actorType="agent"
              actorId={entry.agent.id}
              name={entry.agent.name}
              query={query}
              showStatusDot
              onSelect={() => palette.navigate(p.agentDetail(entry.agent.id))}
              detail={<AgentPresenceText availability={entry.availability} runtimeLabel={entry.runtimeLabel} />}
            />
          ))}
        </PaletteGroup>
      ),
    });
  }
  const squads = objects.squads.filter((hit) => !isBest("squad", hit.item.squad.id));
  if (squads.length > 0) {
    groups.push({
      key: "squads",
      type: "squads",
      score: squads[0]!.score,
      node: (
        <PaletteGroup key="squads" heading={t(($) => $.groups.squads)}>
          {squads.map(({ item: entry }) => (
            <ActorRow
              key={entry.squad.id}
              value={`squad:${entry.squad.id}`}
              actorType="squad"
              actorId={entry.squad.id}
              name={entry.squad.name}
              query={query}
              onSelect={() => palette.navigate(p.squadDetail(entry.squad.id))}
            />
          ))}
        </PaletteGroup>
      ),
    });
  }
  const members = objects.members.filter((hit) => !isBest("member", hit.item.user_id));
  if (members.length > 0) {
    groups.push({
      key: "members",
      type: "members",
      score: members[0]!.score,
      node: (
        <PaletteGroup key="members" heading={t(($) => $.groups.members)}>
          {members.map(({ item: member }) => (
            <ActorRow
              key={member.user_id}
              value={`member:${member.user_id}`}
              actorType="member"
              actorId={member.user_id}
              name={member.name}
              query={query}
              onSelect={() => palette.navigate(p.memberDetail(member.user_id))}
              detail={
                <span className="text-caption text-muted-foreground">
                  <HighlightText text={member.email} query={query} />
                </span>
              }
            />
          ))}
        </PaletteGroup>
      ),
    });
  }

  /*
    Render order keeps the cross-type cancelled partition (MUL-5824): live
    projects and live issues rank with everything else, and one trailing
    Cancelled section holds cancelled projects then cancelled issues. Projects
    and issues arrive as two independently ranked responses, so per-type
    ordering is not enough — keeping cancelled rows in a single trailing
    section is the only arrangement where no cancelled row of either type can
    precede a live row of the other. Direct hits stay in their live section
    (see partitionAggregatedSearchResults).
  */
  const liveProjects = partitioned.liveProjects.filter((project) => !isBest("project", project.id));
  if (liveProjects.length > 0) {
    groups.push({
      key: "projects",
      type: "projects",
      score: model.liveProjectScore,
      node: (
        <PaletteGroup key="projects" heading={t(($) => $.groups.projects)}>
          {liveProjects.map((project) => (
            <ProjectResultRow
              key={project.id}
              project={project}
              query={server.results.query}
              disabled={server.stale}
              onSelect={() => palette.navigate(p.projectDetail(project.id))}
            />
          ))}
        </PaletteGroup>
      ),
    });
  }
  if (partitioned.liveIssues.length > 0) {
    groups.push({
      key: "issues",
      type: "issues",
      score: model.liveIssueScore,
      node: (
        <PaletteGroup key="issues" heading={t(($) => $.groups.issues)}>
          {partitioned.liveIssues.map((issue) => (
            <IssueResultRow
              key={issue.id}
              issue={issue}
              query={server.results.query}
              disabled={server.stale}
              onSelect={() => palette.navigate(p.issueDetail(issue.id))}
            />
          ))}
        </PaletteGroup>
      ),
    });
  }
  if (objects.autopilots.length > 0) {
    groups.push({
      key: "autopilots",
      type: "autopilots",
      score: objects.autopilots[0]!.score,
      node: (
        <PaletteGroup key="autopilots" heading={t(($) => $.groups.autopilots)}>
          {objects.autopilots.map(({ item: autopilot }) => (
            <ObjectRow
              key={autopilot.id}
              value={`autopilot:${autopilot.id}`}
              icon={<Zap className={ROW_ICON_CLASS} />}
              title={autopilot.title}
              query={query}
              onSelect={() => palette.navigate(p.autopilotDetail(autopilot.id))}
            />
          ))}
        </PaletteGroup>
      ),
    });
  }
  if (objects.chats.length > 0) {
    groups.push({
      key: "chats",
      type: "chats",
      score: objects.chats[0]!.score,
      node: (
        <PaletteGroup key="chats" heading={t(($) => $.groups.chats)}>
          {objects.chats.map(({ item: session }) => (
            <ObjectRow
              key={session.id}
              value={`chat:${session.id}`}
              icon={<MessageSquare className={ROW_ICON_CLASS} />}
              title={session.title}
              query={query}
              onSelect={() => palette.navigate(p.chatSession(session.id))}
            />
          ))}
        </PaletteGroup>
      ),
    });
  }
  if (objects.skills.length > 0) {
    groups.push({
      key: "skills",
      type: "skills",
      score: objects.skills[0]!.score,
      node: (
        <PaletteGroup key="skills" heading={t(($) => $.groups.skills)}>
          {objects.skills.map(({ item: skill }) => (
            <ObjectRow
              key={skill.id}
              value={`skill:${skill.id}`}
              icon={<Sparkles className={ROW_ICON_CLASS} />}
              title={skill.name}
              query={query}
              detail={skill.description}
              onSelect={() => palette.navigate(p.skillDetail(skill.id))}
            />
          ))}
        </PaletteGroup>
      ),
    });
  }
  if (objects.workspaces.length > 0) {
    groups.push({
      key: "workspaces",
      type: "workspaces",
      score: objects.workspaces[0]!.score,
      node: (
        <PaletteGroup key="workspaces" heading={t(($) => $.groups.workspaces)}>
          {objects.workspaces.map(({ item: ws }) => (
            <CommandPrimitive.Item
              key={ws.id}
              value={`workspace:${ws.id}`}
              // Web lands on the target's Issues; the desktop adapter reads
              // this same push as "resume the workspace's last active tab".
              onSelect={() => palette.navigate(paths.workspace(ws.slug).issues())}
              className={ROW_CLASS}
            >
              <WorkspaceAvatar name={ws.name} avatarUrl={ws.avatar_url} size="sm" />
              <span className="min-w-0 flex-1 truncate">
                <HighlightText text={ws.name} query={query} />
              </span>
              {/* Same dot the sidebar switcher puts on a workspace with
                  unread inbox items. */}
              {unreadWsIds.has(ws.id) && (
                <span data-testid="workspace-unread-dot" className="size-2 shrink-0 rounded-full bg-brand" />
              )}
            </CommandPrimitive.Item>
          ))}
        </PaletteGroup>
      ),
    });
  }

  groups.sort(
    (a, b) => b.score - a.score || TYPE_ORDER.indexOf(a.type) - TYPE_ORDER.indexOf(b.type),
  );

  const nothing = !model.hasResults && !model.delegate && !best;

  return (
    <>
      {model.delegate && (
        <DelegateGroup
          prompt={model.delegate.prompt}
          actor={model.delegate.actor}
          agentEntry={model.delegate.agentEntry}
          attachments={model.delegate.attachments}
          onSubmit={(actor) => onDelegate(actor, model.delegate!.prompt)}
        />
      )}
      {best && <BestMatchGroups best={best} query={query} target={model.primaryIssueTarget} />}
      {groups.map((group) => group.node)}
      {partitioned.hasCancelled && (
        <PaletteGroup heading={t(($) => $.groups.cancelled)}>
          {partitioned.cancelledProjects.map((project) => (
            <ProjectResultRow
              key={project.id}
              project={project}
              query={server.results.query}
              disabled={server.stale}
              onSelect={() => palette.navigate(p.projectDetail(project.id))}
            />
          ))}
          {partitioned.cancelledIssues.map((issue) => (
            <IssueResultRow
              key={issue.id}
              issue={issue}
              query={server.results.query}
              disabled={server.stale}
              onSelect={() => palette.navigate(p.issueDetail(issue.id))}
            />
          ))}
        </PaletteGroup>
      )}
      {/*
        Spinner only while there is nothing to look at. isLoading flips on the
        keystroke, before the debounce even fires, so swapping the whole list
        for a spinner on every character made it strobe while typing. Keeping
        the previous query's rows until the next response lands is the
        standard trade: they are one keystroke stale for a moment, instead of
        gone.
      */}
      {server.isLoading && nothing && <LoadingRow />}
      {!server.isLoading && nothing && (
        <CommandPrimitive.Empty className="py-10 text-center text-body text-muted-foreground">
          {t(($) => $.empty.no_results)}
        </CommandPrimitive.Empty>
      )}
    </>
  );
}

function LoadingRow() {
  return (
    <div className="flex items-center justify-center py-10">
      <Loader2 className="size-5 animate-spin text-muted-foreground" />
    </div>
  );
}

function BestMatchGroups({
  best,
  query,
  target,
}: {
  best: BestMatch;
  query: string;
  target: IssueTarget | null;
}) {
  const { t } = useT("search");
  const palette = usePalette();
  const p = useWorkspacePaths();

  let row: ReactNode;
  const actions: { id: string; label: string; icon: IconComponent; run?: () => void }[] = [];
  const subject = target
    ? target.kind === "single"
      ? target.issue.identifier
      : t(($) => $.context.selected_issues, { count: target.issues.length })
    : null;

  if (best.type === "agent") {
    const { agent } = best.entry;
    row = (
      <ActorRow
        value={`best:agent:${agent.id}`}
        actorType="agent"
        actorId={agent.id}
        name={agent.name}
        query={query}
        showStatusDot
        onSelect={() => palette.navigate(p.agentDetail(agent.id))}
        detail={<AgentPresenceText availability={best.entry.availability} runtimeLabel={best.entry.runtimeLabel} />}
        trailing={<RowHint>{t(($) => $.types.agent)}</RowHint>}
      />
    );
    if (best.entry.assignable) {
      if (target && subject) {
        actions.push({
          id: "assign",
          label: t(($) => $.best.assign, { subject, name: agent.name }),
          icon: UserPlus,
        });
      }
      actions.push({
        id: "chat",
        label: t(($) => $.best.chat, { name: agent.name }),
        icon: MessageSquare,
        run: () => palette.navigate(p.chatWithAgent(agent.id)),
      });
    }
  } else if (best.type === "squad") {
    const { squad } = best.entry;
    row = (
      <ActorRow
        value={`best:squad:${squad.id}`}
        actorType="squad"
        actorId={squad.id}
        name={squad.name}
        query={query}
        onSelect={() => palette.navigate(p.squadDetail(squad.id))}
        trailing={<RowHint>{t(($) => $.types.squad)}</RowHint>}
      />
    );
    if (best.entry.runnable && target && subject) {
      actions.push({
        id: "assign",
        label: t(($) => $.best.assign, { subject, name: squad.name }),
        icon: UserPlus,
      });
    }
  } else if (best.type === "member") {
    const { member } = best;
    row = (
      <ActorRow
        value={`best:member:${member.user_id}`}
        actorType="member"
        actorId={member.user_id}
        name={member.name}
        query={query}
        onSelect={() => palette.navigate(p.memberDetail(member.user_id))}
        detail={<span className="text-caption text-muted-foreground">{member.email}</span>}
        trailing={<RowHint>{t(($) => $.types.member)}</RowHint>}
      />
    );
    if (target && subject) {
      actions.push({
        id: "assign",
        label: t(($) => $.best.assign, { subject, name: member.name }),
        icon: UserPlus,
      });
    }
  } else {
    const { project } = best;
    row = (
      <ProjectResultRow
        project={project}
        query={query}
        onSelect={() => palette.navigate(p.projectDetail(project.id))}
        trailing={<RowHint>{t(($) => $.types.project)}</RowHint>}
      />
    );
    actions.push({
      id: "new-issue",
      label: t(($) => $.best.new_issue_in, { name: project.title }),
      icon: SquarePen,
      run: () => {
        palette.close();
        openCreateIssueWithPreference({ project_id: project.id });
      },
    });
  }

  return (
    <>
      <PaletteGroup heading={t(($) => $.groups.best_match)}>{row}</PaletteGroup>
      {actions.length > 0 && (
        <PaletteGroup heading={t(($) => $.groups.actions)}>
          {actions.map((action) =>
            action.id === "assign" && target ? (
              <AssignActionRow key={action.id} label={action.label} target={target} best={best} query={query} />
            ) : (
              <CommandPrimitive.Item
                key={action.id}
                value={`best-action:${action.id}`}
                onSelect={action.run}
                className={ROW_CLASS}
              >
                <action.icon className={ROW_ICON_CLASS} />
                <span className="min-w-0 flex-1 truncate">
                  <HighlightText text={action.label} query={query} />
                </span>
              </CommandPrimitive.Item>
            ),
          )}
        </PaletteGroup>
      )}
    </>
  );
}

function AssignActionRow({
  label,
  target,
  best,
  query,
}: {
  label: string;
  target: IssueTarget;
  best: BestMatch;
  query: string;
}) {
  const palette = usePalette();
  const writer = useIssueTargetWriter(target);
  return (
    <CommandPrimitive.Item
      value="best-action:assign"
      onSelect={() => {
        palette.close();
        if (best.type === "agent") writer.assign("agent", best.entry.agent.id);
        else if (best.type === "squad") writer.assign("squad", best.entry.squad.id);
        else if (best.type === "member") writer.assign("member", best.member.user_id);
      }}
      className={ROW_CLASS}
    >
      <UserPlus className={ROW_ICON_CLASS} />
      <span className="min-w-0 flex-1 truncate">
        <HighlightText text={label} query={query} />
      </span>
    </CommandPrimitive.Item>
  );
}

function EmptyState({ model }: { model: RootModel }) {
  const { t } = useT("search");
  const palette = usePalette();
  const p = useWorkspacePaths();

  if (model.scope !== "all") {
    return (
      <>
        {model.contextGroups.map((group) => (
          <CommandGroupView key={group.id} group={group} query="" />
        ))}
        {model.commands.length > 0 && (
          <PaletteGroup heading={t(($) => $.groups.commands)}>
            {model.commands.map(({ item }) => (
              <CommandRow key={item.id} command={item} query="" />
            ))}
          </PaletteGroup>
        )}
        {model.scope !== "commands" && (
          <div className="px-5 py-4 text-center text-caption text-muted-foreground">
            {model.scope === "issues" ? t(($) => $.empty.type_to_find_issue) : t(($) => $.empty.type_to_find_people)}
          </div>
        )}
      </>
    );
  }

  return (
    <>
      {model.contextGroups.map((group) => (
        <CommandGroupView key={group.id} group={group} query="" />
      ))}
      <PaletteGroup heading={t(($) => $.groups.new)}>
        {model.newCommands.map((command) => (
          <CommandRow key={command.id} command={command} query="" />
        ))}
      </PaletteGroup>
      {model.recent.length > 0 && (
        <PaletteGroup
          heading={
            <>
              <Clock className="size-3" />
              <span>{t(($) => $.groups.recent)}</span>
            </>
          }
        >
          {model.recent.map((item) => {
            switch (item.type) {
              case "issue":
                return (
                  <IssueResultRow
                    key={`issue:${item.id}`}
                    issue={item.issue}
                    query=""
                    onSelect={() => palette.navigate(p.issueDetail(item.id))}
                    trailing={<RowHint>{t(($) => $.types.issue)}</RowHint>}
                  />
                );
              case "project":
                return (
                  <ObjectRow
                    key={`project:${item.id}`}
                    value={`recent-project:${item.id}`}
                    icon={<ProjectIcon project={item.project} size="md" />}
                    title={item.project.title}
                    query=""
                    typeLabel={t(($) => $.types.project)}
                    onSelect={() => palette.navigate(p.projectDetail(item.id))}
                  />
                );
              case "agent":
                return (
                  <ObjectRow
                    key={`agent:${item.id}`}
                    value={`recent-agent:${item.id}`}
                    icon={
                      <ActorAvatar
                        actorType="agent"
                        actorId={item.id}
                        size="sm"
                        profileLink={false}
                        showStatusDot
                        className="shrink-0"
                      />
                    }
                    title={item.agent.name}
                    query=""
                    typeLabel={t(($) => $.types.agent)}
                    onSelect={() => palette.navigate(p.agentDetail(item.id))}
                  />
                );
              case "autopilot":
                return (
                  <ObjectRow
                    key={`autopilot:${item.id}`}
                    value={`recent-autopilot:${item.id}`}
                    icon={<Zap className={ROW_ICON_CLASS} />}
                    title={item.autopilot.title}
                    query=""
                    typeLabel={t(($) => $.types.autopilot)}
                    onSelect={() => palette.navigate(p.autopilotDetail(item.id))}
                  />
                );
            }
          })}
        </PaletteGroup>
      )}
    </>
  );
}

