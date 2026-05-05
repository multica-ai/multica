"use client";

import {
  ChevronRight,
  Download,
  FileText,
  HardDrive,
  Lock,
  Pencil,
} from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
  Skill,
} from "@multica/core/types";
import { timeAgo } from "@multica/core/utils";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "@multica/i18n/react";
import { readOrigin, totalFileCount } from "../lib/origin";

// Per-row data assembled at the page level. The columns reach into
// `row.original` and never pull their own queries.
export interface SkillRow {
  skill: Skill;
  agents: Agent[];
  creator: MemberWithUser | null;
  // Originating runtime when the skill was imported from a runtime-local
  // store; null for manually-created or remotely-sourced skills.
  runtime: AgentRuntime | null;
  canEdit: boolean;
}

// Column widths in px. Both Name and Source carry `meta.grow: true`,
// so DataTable skips their inline widths and fixed table-layout splits
// the leftover space between them equally — a single grow column would
// dump all the slack into the Name column, leaving Source perpetually
// truncated while Name accumulates wasteful right-side whitespace.
//
// Each column's `size` is its floor: the values still flow into
// table.getTotalSize() and become the table's min-width, so when the
// viewport drops below the sum, the container scrolls horizontally
// instead of letting either column shrink past its floor.
const COL_WIDTHS = {
  name: 240,
  usedBy: 140,
  source: 220,
  updated: 100,
  // 48 = 16 left padding + 16 chevron icon + 16 right padding. Keeps
  // the chevron's right edge 16px from the card so it lines up with
  // the toolbar's px-4 right inset.
  chevron: 48,
} as const;

export function createSkillColumns(t: (key: string, params?: Record<string, string | number>) => string): ColumnDef<SkillRow>[] {
  return [
    {
      id: "name",
      header: t("col_name"),
      size: COL_WIDTHS.name,
      meta: { grow: true },
      cell: ({ row }) => <SkillNameCell row={row.original} />,
    },
    {
      id: "usedBy",
      header: t("col_used_by"),
      size: COL_WIDTHS.usedBy,
      cell: ({ row }) => <AgentAssignees agents={row.original.agents} />,
    },
    {
      id: "source",
      header: t("col_source"),
      size: COL_WIDTHS.source,
      meta: { grow: true },
      cell: ({ row }) => (
        <SourceCell
          skill={row.original.skill}
          creator={row.original.creator}
          runtime={row.original.runtime}
        />
      ),
    },
    {
      id: "updated",
      header: t("col_updated"),
      size: COL_WIDTHS.updated,
      cell: ({ row }) => (
        <span className="whitespace-nowrap text-xs text-muted-foreground">
          {timeAgo(row.original.skill.updated_at)}
        </span>
      ),
    },
    {
      // Trailing chevron — purely a "this row is clickable" affordance,
      // matches the convention from the pre-data-table SkillRow. The
      // colour deepens on row hover via the row's `group` class.
      id: "_chevron",
      header: () => null,
      size: COL_WIDTHS.chevron,
      enableResizing: false,
      cell: () => (
        <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/40 transition-colors group-hover:text-muted-foreground" />
      ),
    },
  ];
}

// ---------------------------------------------------------------------------
// Cell renderers
// ---------------------------------------------------------------------------

function SkillNameCell({ row }: { row: SkillRow }) {
  const { skill, canEdit } = row;
  const t = useT("skills");
  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-2">
        <span className="block min-w-0 truncate font-medium">{skill.name}</span>
        {!canEdit && (
          <Tooltip>
            <TooltipTrigger
              render={
                <Lock className="h-3 w-3 shrink-0 text-muted-foreground/60" />
              }
            />
            <TooltipContent>
              {t("read_only_tooltip")}
            </TooltipContent>
          </Tooltip>
        )}
        <span className="inline-flex shrink-0 items-center gap-0.5 font-mono text-xs text-muted-foreground/70">
          <FileText className="h-3 w-3" />
          {totalFileCount(skill)}
        </span>
      </div>
      {/* `max-w-xl` (36rem) caps how wide the description gets on
          large viewports. The Name column is `meta.grow`, so on a
          24" desktop it can balloon past 800px — without this cap,
          a long single-line description would stretch all the way
          across, reading more like a paragraph than a table cell. */}
      <div
        className={`mt-0.5 max-w-xl truncate text-xs ${
          skill.description
            ? "text-muted-foreground"
            : "italic text-muted-foreground/50"
        }`}
      >
        {skill.description || t("no_description")}
      </div>
    </div>
  );
}

function AgentAssignees({ agents }: { agents: Agent[] }) {
  const t = useT("skills");
  if (agents.length === 0) {
    return <span className="text-xs text-muted-foreground/70">{t("unused")}</span>;
  }
  const visible = agents.slice(0, 3);
  const extra = agents.length - visible.length;
  return (
    <div className="flex items-center -space-x-1.5">
      {visible.map((a) => (
        <Tooltip key={a.id}>
          <TooltipTrigger
            render={
              <span className="inline-flex rounded-full ring-2 ring-background">
                <ActorAvatar
                  name={a.name}
                  initials={a.name.slice(0, 2).toUpperCase()}
                  avatarUrl={a.avatar_url}
                  isAgent
                  size={22}
                />
              </span>
            }
          />
          <TooltipContent>{a.name}</TooltipContent>
        </Tooltip>
      ))}
      {extra > 0 && (
        <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-muted text-xs font-medium text-muted-foreground ring-2 ring-background">
          +{extra}
        </span>
      )}
    </div>
  );
}

function SourceCell({
  skill,
  creator,
  runtime,
}: {
  skill: Skill;
  creator: MemberWithUser | null;
  runtime: AgentRuntime | null;
}) {
  const t = useT("skills");
  const origin = readOrigin(skill);

  let icon = <Pencil className="h-3 w-3 shrink-0" />;
  let label = t("created_manually");
  if (origin.type === "runtime_local") {
    icon = <HardDrive className="h-3 w-3 shrink-0" />;
    label = runtime
      ? t("from_runtime", { name: runtime.name })
      : origin.provider
        ? t("from_provider_runtime", { provider: origin.provider })
        : t("from_a_runtime");
  } else if (origin.type === "clawhub") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = t("from_clawhub");
  } else if (origin.type === "skills_sh") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = t("from_skills_sh");
  }

  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
        <span className="shrink-0">{icon}</span>
        <span className="block min-w-0 truncate">{label}</span>
      </div>
      {creator && (
        <div className="mt-1 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <ActorAvatar
            name={creator.name}
            initials={creator.name.slice(0, 2).toUpperCase()}
            avatarUrl={creator.avatar_url}
            size={14}
          />
          <span className="truncate">{t("added_by", { name: creator.name })}</span>
        </div>
      )}
    </div>
  );
}
