"use client";

import { useMemo } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace } from "@multica/core/paths";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { autopilotListOptions } from "@multica/core/autopilots/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import {
  agentListOptions,
  memberListOptions,
  skillListOptions,
  squadListOptions,
} from "@multica/core/workspace/queries";
import { buildRuntimeMachines } from "@multica/views/runtimes";
import { resolveSettingsTab } from "@multica/views/settings";
import { formatEntityPageTitle, formatIssuePageTitle } from "@/lib/page-title";
import { PageTitle } from "./page-title";

function isIssueIdentifier(value: string) {
  return /^[A-Za-z][A-Za-z0-9]*-\d+$/.test(value);
}

function findRuntimeMachine(
  machines: ReturnType<typeof buildRuntimeMachines>,
  locator: string,
) {
  return machines.find(
    (candidate) =>
      candidate.id === locator ||
      candidate.runtimes.some((runtime) => runtime.id === locator),
  );
}

type DetailKind =
  | "issue"
  | "project"
  | "agent"
  | "autopilot"
  | "runtime"
  | "skill"
  | "squad"
  | "member";

interface RouteTitle {
  fallback: string;
  detail?: { kind: DetailKind; id: string };
}

const SETTINGS_TITLES: Record<string, string> = {
  profile: "Profile",
  preferences: "Preferences",
  shortcuts: "Keyboard shortcuts",
  issue: "Issue settings",
  chat: "Chat settings",
  notifications: "Notifications",
  tokens: "Tokens",
  workspace: "Workspace",
  repositories: "Repositories",
  github: "GitHub",
  integrations: "Integrations",
  labs: "Labs",
  members: "Members",
  labels: "Labels",
  properties: "Properties",
  "quick-actions": "Quick actions",
};

const SETTINGS_TAB_KEYS = Object.keys(SETTINGS_TITLES);

export function dashboardRouteTitle(pathname: string, settingsTab: string | null): RouteTitle {
  // The first pathname part is the workspace slug for dashboard routes.
  const [section, id, nested] = pathname.split("/").filter(Boolean).slice(1);

  switch (section) {
    case "issues":
      return id
        ? {
            fallback: isIssueIdentifier(id) ? formatIssuePageTitle(id) : "Issue",
            detail: { kind: "issue", id },
          }
        : { fallback: "Issues" };
    case "my-issues": return { fallback: "My issues" };
    case "projects":
      return id ? { fallback: "Project", detail: { kind: "project", id } } : { fallback: "Projects" };
    case "agents":
      if (id === "new") return { fallback: "New agent" };
      return id ? { fallback: "Agent", detail: { kind: "agent", id } } : { fallback: "Agents" };
    case "autopilots":
      return id ? { fallback: "Autopilot", detail: { kind: "autopilot", id } } : { fallback: "Autopilots" };
    case "runtimes":
      if (nested === "runtime") return { fallback: "Runtime settings" };
      return id ? { fallback: "Runtime", detail: { kind: "runtime", id } } : { fallback: "Runtimes" };
    case "skills":
      return id ? { fallback: "Skill", detail: { kind: "skill", id } } : { fallback: "Skills" };
    case "squads":
      return id ? { fallback: "Squad", detail: { kind: "squad", id } } : { fallback: "Squads" };
    case "members":
      return id ? { fallback: "Member", detail: { kind: "member", id } } : { fallback: "Members" };
    case "settings": {
      const tabKey = resolveSettingsTab(settingsTab, SETTINGS_TAB_KEYS);
      const tab = SETTINGS_TITLES[tabKey];
      return { fallback: tab ? `Settings · ${tab}` : "Settings" };
    }
    case "inbox": return { fallback: "Inbox" };
    case "chat": return { fallback: "Chat" };
    case "billing": return { fallback: "Billing" };
    case "usage": return { fallback: "Usage" };
    default: return { fallback: "Workspace" };
  }
}

function entityName(value: unknown): string | undefined {
  if (!value || typeof value !== "object") return undefined;
  const entity = value as Record<string, unknown>;
  for (const key of ["name", "title", "display_name", "label"]) {
    const candidate = entity[key];
    if (typeof candidate === "string" && candidate.trim()) return candidate;
  }
  return undefined;
}

/**
 * Owns tab titles for the authenticated web shell. The fallback appears on
 * navigation; detail queries then add the most useful entity signal.
 * Public/landing pages keep Multica brand titles from root metadata.
 */
export function DashboardPageTitle() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const workspace = useCurrentWorkspace();
  const workspaceId = workspace?.id ?? "";
  const route = useMemo(
    () => dashboardRouteTitle(pathname, searchParams.get("tab")),
    [pathname, searchParams],
  );
  const detail = route.detail;
  const isDetail = (kind: DetailKind) => detail?.kind === kind;
  const isWorkspaceDetail = (kind: DetailKind) => Boolean(workspaceId) && isDetail(kind);

  const { data: issue } = useQuery({
    ...issueDetailOptions(workspaceId, isDetail("issue") ? detail?.id ?? "" : ""),
    enabled: isWorkspaceDetail("issue"),
  });
  const { data: project } = useQuery({
    ...projectDetailOptions(workspaceId, isDetail("project") ? detail?.id ?? "" : ""),
    enabled: isWorkspaceDetail("project"),
  });
  const { data: agents = [] } = useQuery({ ...agentListOptions(workspaceId), enabled: isWorkspaceDetail("agent") });
  const { data: autopilots = [] } = useQuery({ ...autopilotListOptions(workspaceId), enabled: isWorkspaceDetail("autopilot") });
  const { data: runtimes = [] } = useQuery({ ...runtimeListOptions(workspaceId), enabled: isWorkspaceDetail("runtime") });
  const { data: skills = [] } = useQuery({ ...skillListOptions(workspaceId), enabled: isWorkspaceDetail("skill") });
  const { data: squads = [] } = useQuery({ ...squadListOptions(workspaceId), enabled: isWorkspaceDetail("squad") });
  const { data: members = [] } = useQuery({ ...memberListOptions(workspaceId), enabled: isWorkspaceDetail("member") });

  let title = route.fallback;
  if (isDetail("issue")) {
    const routeIdentifier = detail && isIssueIdentifier(detail.id) ? detail.id : undefined;
    title = formatIssuePageTitle(issue?.identifier ?? routeIdentifier, issue?.title);
  }
  if (isDetail("project")) title = formatEntityPageTitle("Project", entityName(project));
  if (isDetail("agent")) title = formatEntityPageTitle("Agent", entityName(agents.find((agent) => agent.id === detail?.id)));
  if (isDetail("autopilot")) title = formatEntityPageTitle("Autopilot", entityName(autopilots.find((autopilot) => autopilot.id === detail?.id)));
  if (isDetail("runtime")) {
    const locator = detail?.id ?? "";
    const machine = findRuntimeMachine(
      buildRuntimeMachines(runtimes, { now: Date.now() }),
      locator,
    );
    title = formatEntityPageTitle("Runtime", entityName(machine));
  }
  if (isDetail("skill")) title = formatEntityPageTitle("Skill", entityName(skills.find((skill) => skill.id === detail?.id)));
  if (isDetail("squad")) title = formatEntityPageTitle("Squad", entityName(squads.find((squad) => squad.id === detail?.id)));
  if (isDetail("member")) title = formatEntityPageTitle("Member", entityName(members.find((member) => member.user_id === detail?.id)));

  return <PageTitle title={title} />;
}
