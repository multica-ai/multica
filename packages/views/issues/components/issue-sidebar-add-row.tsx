"use client";

import { useQuery } from "@tanstack/react-query";
import { Bell, GitPullRequest, Plus, Server, type IconComponent } from "@multica/ui/icons";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { githubInstallationsOptions, issuePullRequestsOptions } from "@multica/core/github";
import { issueEnvironmentsOptions } from "@multica/core/issues/queries";
import { issueWakeupsOptions } from "@multica/core/issues";
import { vcsConnectionsOptions } from "@multica/core/vcs/queries";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useIssueSidebarSectionsStore } from "@multica/core/issues/stores";
import { useT } from "../../i18n";

/** Sections that hide while empty and are offered from the add row instead. */
export type IssueSidebarAddable = "wakeups" | "pull_requests" | "environments";

const ADDABLE_ORDER: readonly IssueSidebarAddable[] = ["wakeups", "pull_requests", "environments"];

type AddRowLabel =
  | IssueSidebarAddable
  | "wakeups_pull_requests"
  | "wakeups_environments"
  | "pull_requests_environments"
  | "all";

const ADDABLE_ICON: Record<IssueSidebarAddable, IconComponent> = {
  wakeups: Bell,
  pull_requests: GitPullRequest,
  environments: Server,
};

/**
 * The empty sections this reader could start on this issue. Each one shares
 * its section's query, so the answer matches what the section would render.
 */
export function useIssueSidebarAddables({
  issueId,
  closed,
  enabled,
  pullRequestsAvailable,
}: {
  issueId: string;
  closed: boolean;
  /** False for readers who cannot add anything, such as guests. */
  enabled: boolean;
  /** The workspace shows pull requests in the sidebar at all. */
  pullRequestsAvailable: boolean;
}): IssueSidebarAddable[] {
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const hidden = useIssueSidebarSectionsStore((s) => s.hidden);
  const wakeups = useQuery({ ...issueWakeupsOptions(wsId, issueId), enabled: enabled && !!wsId });
  const prs = useQuery({ ...issuePullRequestsOptions(issueId), enabled: enabled && pullRequestsAvailable });
  const installations = useQuery({ ...githubInstallationsOptions(wsId), enabled: enabled && pullRequestsAvailable && !!wsId });
  const vcsConnections = useQuery({ ...vcsConnectionsOptions(wsId), enabled: enabled && pullRequestsAvailable && !!wsId });
  const environments = useQuery({ ...issueEnvironmentsOptions(wsId, issueId), enabled: enabled && !!wsId });
  if (!enabled) return [];

  // A workspace with no repository to open a pull request against has none to
  // link. Self-hosted GitLab / Gitea / Forgejo merge requests link by URL too.
  const hostsCode =
    (workspace?.repos?.length ?? 0) > 0 ||
    (installations.data?.installations?.length ?? 0) > 0 ||
    (vcsConnections.data?.connections?.length ?? 0) > 0;
  const env = environments.data;
  const can: Record<IssueSidebarAddable, boolean> = {
    wakeups: !closed && wakeups.isSuccess && wakeups.data.length === 0,
    pull_requests:
      pullRequestsAvailable && hostsCode && prs.isSuccess && !!prs.data.auto_complete && prs.data.pull_requests.length === 0,
    // Only a run that declared environments leaves something to start here.
    environments:
      environments.isSuccess &&
      !!env &&
      env.environments.length === 0 &&
      (env.pending_starts ?? []).length === 0 &&
      (env.failed_starts ?? []).length === 0 &&
      (env.configs ?? []).some((config) => config.environments.length > 0),
  };
  return ADDABLE_ORDER.filter((id) => can[id] && !hidden.includes(id));
}

/**
 * One quiet row after the last section that starts any of `items`. A single
 * item opens its form directly; several open a menu to pick from.
 */
export function IssueSidebarAddRow({
  items,
  onPick,
}: {
  items: readonly IssueSidebarAddable[];
  onPick: (item: IssueSidebarAddable) => void;
}) {
  const { t } = useT("issues");
  if (items.length === 0) return null;
  // Whole sentences per combination: languages inflect and space the list differently.
  const key = items.length === ADDABLE_ORDER.length ? "all" : (items.join("_") as AddRowLabel);
  const label = t(($) => $.detail.add_row[key]);
  const rowClass =
    "flex h-7 max-w-full items-center gap-2.5 rounded-md px-2 text-caption text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring data-popup-open:bg-accent/50 data-popup-open:text-foreground";
  const content = (
    <>
      <Plus aria-hidden="true" className="size-3.5 shrink-0" />
      <span className="truncate">{label}</span>
    </>
  );
  return (
    <div data-testid="issue-sidebar-add-row" className="px-2">
      {items.length === 1 ? (
        <button type="button" className={rowClass} onClick={() => onPick(items[0]!)}>
          {content}
        </button>
      ) : (
        <DropdownMenu>
          <DropdownMenuTrigger render={<button type="button" className={rowClass} />}>{content}</DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-56">
            {items.map((id) => {
              const Icon = ADDABLE_ICON[id];
              return (
                <DropdownMenuItem key={id} onClick={() => onPick(id)}>
                  <Icon />
                  {t(($) => $.detail.add_row.menu[id])}
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}
