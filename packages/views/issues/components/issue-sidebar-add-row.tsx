"use client";

import { useQuery } from "@tanstack/react-query";
import { Bell, GitPullRequest, Network, Plus, Server, type IconComponent } from "@multica/ui/icons";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { githubInstallationsOptions, issuePullRequestsOptions } from "@multica/core/github";
import { issueDetailOptions, issueDuplicatesOptions, issueEnvironmentsOptions } from "@multica/core/issues/queries";
import { issueWakeupsOptions } from "@multica/core/issues";
import { vcsConnectionsOptions } from "@multica/core/vcs/queries";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useIssueSidebarSectionsStore } from "@multica/core/issues/stores";
import type { Issue } from "@multica/core/types";
import { useT } from "../../i18n";
import { issueRelations } from "../utils/relations";
import { IssueRelationTypeItems } from "./issue-blockers";

/** Sections that hide while empty and are offered from the add row instead. */
export type IssueSidebarAddable = "wakeups" | "relations" | "pull_requests" | "environments";

const ADDABLE_ORDER: readonly IssueSidebarAddable[] = ["wakeups", "relations", "pull_requests", "environments"];

const ADDABLE_ICON: Record<IssueSidebarAddable, IconComponent> = {
  wakeups: Bell,
  relations: Network,
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
  const issue = useQuery({ ...issueDetailOptions(wsId, issueId), enabled: enabled && !!wsId });
  const duplicates = useQuery({ ...issueDuplicatesOptions(wsId, issueId), enabled: enabled && !!wsId });
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
    // Offered while the Relations section has nothing to list and so hides.
    relations:
      issue.isSuccess &&
      issue.data.blocked_by !== undefined &&
      duplicates.isSuccess &&
      issueRelations(issue.data, duplicates.data.duplicates).empty,
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
 * item opens its form directly; several open a menu to pick from. Relations
 * have no form of their own: they open the relation types, the same items as
 * the Relations section's "+", which need the issue.
 */
export function IssueSidebarAddRow({
  items,
  onPick,
  issue,
}: {
  items: readonly IssueSidebarAddable[];
  onPick: (item: IssueSidebarAddable) => void;
  issue?: Issue;
}) {
  const { t } = useT("issues");
  const shown = issue ? items : items.filter((id) => id !== "relations");
  if (shown.length === 0) return null;
  // One or two items are named; past that the menu names them, so the label
  // does not grow a sentence for every combination.
  const label =
    shown.length === 1
      ? t(($) => $.detail.add_row[shown[0]!])
      : shown.length === 2
        ? t(($) => $.detail.add_row.pair, {
            first: t(($) => $.detail.add_row.noun[shown[0]!]),
            second: t(($) => $.detail.add_row.noun[shown[1]!]),
          })
        : t(($) => $.detail.add_row.many);
  const rowClass =
    "flex h-7 max-w-full items-center gap-2.5 rounded-md px-2 text-caption text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring data-popup-open:bg-accent/50 data-popup-open:text-foreground";
  const content = (
    <>
      <Plus aria-hidden="true" className="size-3.5 shrink-0" />
      <span className="truncate">{label}</span>
    </>
  );
  const only = shown.length === 1 ? shown[0]! : null;
  return (
    <div data-testid="issue-sidebar-add-row" className="px-2">
      {only && only !== "relations" ? (
        <button type="button" className={rowClass} onClick={() => onPick(only)}>
          {content}
        </button>
      ) : (
        <DropdownMenu>
          <DropdownMenuTrigger render={<button type="button" className={rowClass} />}>{content}</DropdownMenuTrigger>
          <DropdownMenuContent align="start" className={only ? "w-64" : "w-56"}>
            {only ? (
              <IssueRelationTypeItems issue={issue!} />
            ) : (
              shown.map((id) => {
                const Icon = ADDABLE_ICON[id];
                const name = t(($) => $.detail.add_row.menu[id]);
                return id === "relations" ? (
                  <DropdownMenuSub key={id}>
                    <DropdownMenuSubTrigger>
                      <Icon />
                      {name}
                    </DropdownMenuSubTrigger>
                    <DropdownMenuSubContent className="w-64">
                      <IssueRelationTypeItems issue={issue!} />
                    </DropdownMenuSubContent>
                  </DropdownMenuSub>
                ) : (
                  <DropdownMenuItem key={id} onClick={() => onPick(id)}>
                    <Icon />
                    {name}
                  </DropdownMenuItem>
                );
              })
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}
