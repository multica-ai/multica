"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { BookOpen, ChevronRight, HardDrive, Users } from "lucide-react";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
  SkillSummary,
} from "@multica/core/types";
import type { SkillOriginType } from "@multica/core/skills/stores";
import { DataTableColumnHeader } from "@multica/ui/components/ui/data-table-column-header";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useT } from "../../i18n";

export interface SkillRow {
  skill: SkillSummary;
  agents: Agent[];
  creator: MemberWithUser | null;
  runtime: AgentRuntime | null;
  originType: SkillOriginType;
  canEdit: boolean;
}

const COL_WIDTHS = {
  name: 320,
  usedBy: 160,
  source: 170,
  creator: 150,
  updated: 110,
  created: 110,
  chevron: 44,
} as const;

function formatShortDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: date.getFullYear() === new Date().getFullYear() ? undefined : "numeric",
  });
}

function SourceCell({ row }: { row: SkillRow }) {
  const { t } = useT("skills");
  if (row.originType === "runtime_local") {
    return (
      <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
        <HardDrive className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">
          {row.runtime
            ? t(($) => $.table.source_runtime_named, { name: row.runtime.name })
            : t(($) => $.table.source_runtime_unknown)}
        </span>
      </span>
    );
  }

  const label =
    row.originType === "clawhub"
      ? t(($) => $.table.source_clawhub)
      : row.originType === "skills_sh"
        ? t(($) => $.table.source_skills_sh)
        : row.originType === "github"
          ? t(($) => $.table.source_github)
          : t(($) => $.table.source_manual);

  return <span className="truncate text-xs text-muted-foreground">{label}</span>;
}

function UsedByCell({ agents }: { agents: Agent[] }) {
  const { t } = useT("skills");
  if (agents.length === 0) {
    return <span className="text-xs text-muted-foreground/40">—</span>;
  }
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
      <Users className="h-3.5 w-3.5 shrink-0" />
      <span className="truncate">
        {agents.length === 1
          ? agents[0]?.name
          : `${t(($) => $.table.used_by)} ${agents.length}`}
      </span>
    </span>
  );
}

function CreatorCell({ member }: { member: MemberWithUser | null }) {
  if (!member) {
    return <span className="text-xs text-muted-foreground/40">—</span>;
  }
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
      <ActorAvatar
        name={member.name}
        initials={member.name.slice(0, 2).toUpperCase()}
        avatarUrl={resolvePublicFileUrl(member.avatar_url)}
        size={18}
      />
      <span className="truncate">{member.name}</span>
    </span>
  );
}

export function useSkillColumns(): ColumnDef<SkillRow>[] {
  const { t } = useT("skills");

  return [
    {
      id: "name",
      accessorFn: (row) => row.skill.name,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} label={t(($) => $.table.name)} />
      ),
      size: COL_WIDTHS.name,
      minSize: 220,
      meta: { grow: true },
      enableSorting: true,
      cell: ({ row }) => (
        <div className="flex min-w-0 items-center gap-2">
          <BookOpen className="h-4 w-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0">
            <div className="truncate text-sm font-medium">
              {row.original.skill.name}
            </div>
            {row.original.skill.description ? (
              <div className="truncate text-xs text-muted-foreground">
                {row.original.skill.description}
              </div>
            ) : null}
          </div>
        </div>
      ),
    },
    {
      id: "usedBy",
      accessorFn: (row) => row.agents.length,
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          label={t(($) => $.table.used_by)}
        />
      ),
      size: COL_WIDTHS.usedBy,
      enableSorting: true,
      cell: ({ row }) => <UsedByCell agents={row.original.agents} />,
    },
    {
      id: "source",
      accessorFn: (row) => row.originType,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} label={t(($) => $.table.source)} />
      ),
      size: COL_WIDTHS.source,
      enableSorting: true,
      cell: ({ row }) => <SourceCell row={row.original} />,
    },
    {
      id: "creator",
      accessorFn: (row) => row.creator?.name ?? "",
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          label={t(($) => $.table.created_by)}
        />
      ),
      size: COL_WIDTHS.creator,
      enableSorting: true,
      cell: ({ row }) => <CreatorCell member={row.original.creator} />,
    },
    {
      id: "updated",
      accessorFn: (row) => row.skill.updated_at,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} label={t(($) => $.table.updated)} />
      ),
      size: COL_WIDTHS.updated,
      enableSorting: true,
      cell: ({ row }) => (
        <span className="whitespace-nowrap text-xs tabular-nums text-muted-foreground">
          {formatShortDate(row.original.skill.updated_at)}
        </span>
      ),
    },
    {
      id: "created",
      accessorFn: (row) => row.skill.created_at,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} label={t(($) => $.table.created)} />
      ),
      size: COL_WIDTHS.created,
      enableSorting: true,
      cell: ({ row }) => (
        <span className="whitespace-nowrap text-xs tabular-nums text-muted-foreground">
          {formatShortDate(row.original.skill.created_at)}
        </span>
      ),
    },
    {
      id: "_chevron",
      size: COL_WIDTHS.chevron,
      enableSorting: false,
      cell: () => (
        <ChevronRight className="ml-auto h-4 w-4 text-muted-foreground/50" />
      ),
    },
  ];
}
