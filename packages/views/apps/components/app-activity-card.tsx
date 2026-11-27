"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import { appActivityOptions } from "@multica/core/connected-apps";
import { issueDetailOptions } from "@multica/core/issues/queries";
import type { AppAccountScope, AppToolCall } from "@multica/core/types";
import { toolLabel } from "../../issues/components/app-call-chips";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import { ObjectSection, ObjectSectionCard, ObjectSectionEmpty } from "../../layout/object-page";

const LIMIT = 10;

type ScopedCall = AppToolCall & { scope: AppAccountScope };

/**
 * The latest calls agents made to this app, through the team account (only
 * admins can read it) and through the viewer's own.
 */
export function AppActivityCard({ slug, name, canManage }: { slug: string; name: string; canManage: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const team = useQuery({ ...appActivityOptions(wsId, slug, "team"), enabled: canManage });
  const personal = useQuery(appActivityOptions(wsId, slug, "personal"));
  const loading = personal.isLoading || (canManage && team.isLoading);
  const calls: ScopedCall[] = [
    ...(canManage ? team.data ?? [] : []).map((c) => ({ ...c, scope: "team" as const })),
    ...(personal.data ?? []).map((c) => ({ ...c, scope: "personal" as const })),
  ]
    .sort((a, b) => b.created_at.localeCompare(a.created_at))
    .slice(0, LIMIT);

  return (
    <ObjectSection title={t(($) => $.connected_apps.app_page.activity_title)}>
      {loading ? (
        <ObjectSectionEmpty description={t(($) => $.connected_apps.loading)} />
      ) : personal.isError || team.isError ? (
        <ObjectSectionEmpty description={t(($) => $.connected_apps.detail.activity_failed)} />
      ) : calls.length === 0 ? (
        <ObjectSectionEmpty description={t(($) => $.connected_apps.app_page.activity_empty, { app: name })} />
      ) : (
        <ObjectSectionCard>
          {calls.map((call) => <ActivityRow key={`${call.scope}:${call.id}`} call={call} slug={slug} />)}
        </ObjectSectionCard>
      )}
    </ObjectSection>
  );
}

function ActivityRow({ call, slug }: { call: ScopedCall; slug: string }) {
  const { t } = useT("settings");
  const timeAgo = useTimeAgo();
  const { getActorName } = useActorName();
  const failed = call.outcome === "error" || call.outcome === "blocked";
  return (
    <div className="flex items-baseline gap-3 px-3 py-2 text-body">
      <span className="min-w-0 flex-1 truncate">
        <span className="font-medium">{getActorName("agent", call.agent_id)}</span>{" "}
        <span className={failed ? "text-destructive" : "text-muted-foreground"}>
          {toolLabel(slug, call.tool_slug)}
        </span>
      </span>
      <span className="shrink-0 text-caption text-muted-foreground">
        {call.scope === "team"
          ? t(($) => $.connected_apps.app_page.as_team)
          : t(($) => $.connected_apps.app_page.as_you)}
        {" · "}
        {call.issue_id ? <IssueRef issueId={call.issue_id} /> : t(($) => $.connected_apps.app_page.in_chat)}
        {" · "}
        {timeAgo(call.created_at)}
      </span>
    </div>
  );
}

function IssueRef({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const issue = useQuery(issueDetailOptions(wsId, issueId));
  return (
    <AppLink href={paths.issueDetail(issueRouteRef({ id: issueId, identifier: issue.data?.identifier }))} className="hover:text-foreground hover:underline">
      {issue.data?.identifier ?? "…"}
    </AppLink>
  );
}
