"use client";

import type { Agent, MemberWithUser, Squad } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { ActorAvatar } from "../../common/actor-avatar";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { SquadAvatar, SquadRowActions } from "./squads-page";

export interface SquadEnhancedRowsProps {
  squads: Squad[];
  agentsById: Map<string, Agent>;
  membersById: Map<string, MemberWithUser>;
  locale: string;
  /** Whether the current user is a workspace admin. */
  isWorkspaceAdmin: boolean;
  /** Current user id, for per-squad creator check. */
  currentUserId: string | undefined;
  /** Whether any visible squad can be managed (gates the kebab track). */
  canManageAnyRow: boolean;
}

/**
 * Enhanced rows view for the squads list — one card-like row per squad, with
 * the squad leader avatar rendered LARGER (lg=32px) than member avatars
 * (sm=20px) to visually distinguish the leader from the members.
 *
 * No view-mode toggle: the squads page always renders this layout.
 */
export function SquadEnhancedRows({
  squads,
  agentsById,
  membersById,
  locale,
  isWorkspaceAdmin,
  currentUserId,
  canManageAnyRow,
}: SquadEnhancedRowsProps) {
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const { t } = useT("squads");

  if (squads.length === 0) return null;

  return (
    <div className="flex flex-col gap-2" role="list">
      {squads.map((squad) => {
        const leader = agentsById.get(squad.leader_id);
        const leaderName = leader?.name ?? squad.leader_id.slice(0, 8);

        const preview = squad.member_preview ?? [];
        const totalCount = squad.member_count ?? preview.length;
        const visibleMembers = preview.slice(0, 5);
        const overflow = totalCount - visibleMembers.length;

        const canManageThisRow =
          isWorkspaceAdmin ||
          (!!currentUserId && squad.creator_id === currentUserId);

        return (
          <div
            key={squad.id}
            className="group/row grid w-full cursor-pointer items-center gap-4 rounded-lg border border-surface-border bg-surface px-5 py-3.5 transition-colors hover:bg-surface-hover"
            role="listitem"
            onClick={() => navigation.push(paths.squadDetail(squad.id))}
          >
            <div className="grid w-full grid-cols-[56px_minmax(220px,1.2fr)_auto_auto_minmax(140px,auto)] items-center gap-4">
              {/* [1] Squad avatar */}
              <SquadAvatar squad={squad} />

              {/* [2] Identity — name + description */}
              <div className="flex min-w-0 flex-col gap-0.5">
                <span className="min-w-0 truncate text-body font-semibold">
                  {squad.name}
                </span>
                {squad.description ? (
                  <span className="min-w-0 truncate text-caption text-muted-foreground">
                    {squad.description}
                  </span>
                ) : null}
              </div>

              {/* [3] Leader — lg avatar (32px), LARGER than member avatars */}
              <div className="flex items-center gap-2">
                <ActorAvatar
                  actorType="agent"
                  actorId={squad.leader_id}
                  size="lg"
                  enableHoverCard
                />
                <div className="flex min-w-0 flex-col">
                  <span className="text-micro text-faint-foreground">
                    {t(($) => $.page.table.leader)}
                  </span>
                  <span className="min-w-0 truncate text-caption text-muted-foreground">
                    {leaderName}
                  </span>
                </div>
              </div>

              {/* [4] Members — sm avatars (20px), SMALLER than leader avatar */}
              <div className="flex items-center gap-2">
                {totalCount === 0 ? (
                  <span className="text-caption text-faint-foreground">—</span>
                ) : (
                  <div className="flex flex-col gap-0.5">
                    <span className="text-micro text-faint-foreground">
                      {t(($) => $.page.table.members)}
                      <span className="ml-1 font-mono tabular-nums">
                        {totalCount}
                      </span>
                    </span>
                    <div className="flex items-center -space-x-1.5">
                      {visibleMembers.map((m) => (
                        <span
                          key={`${m.member_type}-${m.member_id}`}
                          className="inline-flex rounded-full ring-2 ring-surface"
                        >
                          <ActorAvatar
                            actorType={m.member_type}
                            actorId={m.member_id}
                            size="sm"
                            enableHoverCard={m.member_type === "agent"}
                          />
                        </span>
                      ))}
                      {overflow > 0 && (
                        <span className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-muted text-micro font-medium text-muted-foreground ring-2 ring-surface">
                          +{overflow}
                        </span>
                      )}
                    </div>
                  </div>
                )}
              </div>

              {/* [5] Creator + Created + kebab — de-emphasized right zone */}
              <div className="flex items-center justify-end gap-3">
                <div className="flex flex-col items-end gap-0.5">
                  <div className="flex items-center gap-1">
                    <ActorAvatar
                      actorType="member"
                      actorId={squad.creator_id}
                      size="xs"
                    />
                    <span className="max-w-[80px] truncate text-micro text-faint-foreground">
                      {membersById.get(squad.creator_id)?.name ??
                        squad.creator_id.slice(0, 8)}
                    </span>
                  </div>
                  <span className="font-mono text-micro tabular-nums text-faint-foreground">
                    {new Date(squad.created_at).toLocaleDateString(locale)}
                  </span>
                </div>
                {canManageAnyRow && canManageThisRow && (
                  <SquadRowActions squad={squad} />
                )}
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
