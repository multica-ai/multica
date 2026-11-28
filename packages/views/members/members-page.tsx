"use client";

import { useMemo, useState } from "react";
import { AlertCircle, Settings, UserPlus, UserRound } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useQuery } from "@tanstack/react-query";
import type { MemberRole, MemberWithUser } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import { ActorAvatar } from "../common/actor-avatar";
import {
  CollectionDisplayButton,
  CollectionNoMatches,
  CollectionPageHeader,
  CollectionPageState,
  CollectionSearch,
} from "../layout/collection-page";
import {
  CollectionList,
  CollectionNameCell,
  type CollectionColumn,
} from "../layout/collection-list";
import { useNavigation } from "../navigation";
import { useLocale, useT } from "../i18n";

const ROLE_ORDER: Record<MemberRole, number> = {
  owner: 0,
  admin: 1,
  member: 2,
  guest: 3,
};

function matches(member: MemberWithUser, query: string) {
  if (!query) return true;
  return (
    member.name.toLowerCase().includes(query) ||
    member.email.toLowerCase().includes(query)
  );
}

/**
 * The workspace's people, opened from the Team panel: each row leads to the
 * member's page. Inviting and changing roles stay in Settings → Members, which
 * admins reach from the header.
 */
export function MembersPage() {
  const { t } = useT("members");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { push } = useNavigation();
  const userId = useAuthStore((s) => s.user?.id);
  const membersQuery = useQuery(memberListOptions(wsId));
  const members = useMemo(() => membersQuery.data ?? [], [membersQuery.data]);
  // A failed background refresh keeps the cached list; only a first load that
  // failed with nothing to show becomes an error.
  const loadFailed = membersQuery.isError && membersQuery.data === undefined;
  const [search, setSearch] = useState("");

  const myRole = members.find((m) => m.user_id === userId)?.role;
  const isAdmin = myRole === "owner" || myRole === "admin";
  const query = search.trim().toLowerCase();

  const rows = useMemo(
    () =>
      members
        .filter((m) => matches(m, query))
        .sort(
          (a, b) =>
            (ROLE_ORDER[a.role] ?? 9) - (ROLE_ORDER[b.role] ?? 9) ||
            a.name.localeCompare(b.name, locale),
        ),
    [members, query, locale],
  );

  const columns = useMemo<CollectionColumn<MemberWithUser>[]>(
    () => [
      {
        key: "name",
        header: t(($) => $.page.table.name),
        cell: (member) => (
          <CollectionNameCell
            icon={
              <ActorAvatar
                actorType="member"
                actorId={member.user_id}
                size="sm"
                profileLink={false}
              />
            }
            title={member.name}
            badges={
              member.user_id === userId ? (
                <span className="text-caption text-muted-foreground">
                  {t(($) => $.page.you)}
                </span>
              ) : undefined
            }
            description={member.email}
          />
        ),
      },
      {
        key: "role",
        header: t(($) => $.page.table.role),
        width: 112,
        core: true,
        cell: (member) => (
          <span className="truncate">{t(($) => $.role[member.role])}</span>
        ),
      },
      {
        key: "joined",
        header: t(($) => $.page.table.joined),
        width: 104,
        cell: (member) => {
          const joined = new Date(member.created_at);
          return Number.isFinite(joined.getTime()) ? (
            <span className="tabular-nums">
              {joined.toLocaleDateString(locale)}
            </span>
          ) : null;
        },
      },
    ],
    [t, locale, userId],
  );

  const title = t(($) => $.page.title);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={UserRound}
        title={title}
        tools={
          <CollectionSearch
            value={search}
            onChange={setSearch}
            label={t(($) => $.page.search)}
          />
        }
        actions={
          isAdmin ? (
            <>
              <CollectionDisplayButton
                icon={Settings}
                label={t(($) => $.page.manage)}
                onClick={() => push(`${p.settings()}?tab=members`)}
              />
              <CollectionDisplayButton
                icon={UserPlus}
                label={t(($) => $.page.invite)}
                onClick={() => push(`${p.settings()}?tab=members&invite=1`)}
              />
            </>
          ) : undefined
        }
      />
      {loadFailed ? (
        <CollectionPageState
          tone="destructive"
          icon={AlertCircle}
          role="alert"
          title={t(($) => $.page.load_failed)}
          actions={
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={membersQuery.isFetching}
              onClick={() => membersQuery.refetch()}
            >
              {t(($) => $.page.retry)}
            </Button>
          }
        />
      ) : (
        <CollectionList
          label={title}
          columns={columns}
          rows={rows}
          loading={membersQuery.isLoading}
          getRowId={(member) => member.user_id}
          getRowHref={(member) => p.memberDetail(member.user_id)}
          getRowTitle={(member) => member.name}
          empty={
            <CollectionNoMatches
              title={t(($) => $.page.no_matches)}
              onReset={() => setSearch("")}
              hasSearch={query !== ""}
            />
          }
        />
      )}
    </div>
  );
}
