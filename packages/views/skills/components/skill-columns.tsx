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
import { readOrigin, totalFileCount } from "../lib/origin";
import { useT } from "../../i18n";

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

const COL_WIDTHS = {
  name: 240,
  usedBy: 140,
  source: 220,
  updated: 100,
  chevron: 48,
} as const;

// Hook returns column defs that close over a translation function. Defining
// columns inside a hook (rather than as a module-level static) is the i18n
// price for header strings — same pattern used by inbox `useTypeLabels`.
export function useSkillColumns(): ColumnDef<SkillRow>[] {
  const { t } = useT("skills");
  return [
    {
      id: "name",
      header: t(($) => $.table.name),
      size: COL_WIDTHS.name,
      meta: { grow: true },
      cell: ({ row }) => <SkillNameCell row={row.original} />,
    },
    {
      id: "usedBy",
      header: t(($) => $.table.used_by),
      size: COL_WIDTHS.usedBy,
      cell: ({ row }) => <AgentAssignees agents={row.original.agents} />,
    },
    {
      id: "source",
      header: t(($) => $.table.source),
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
      header: t(($) => $.table.updated),
      size: COL_WIDTHS.updated,
      cell: ({ row }) => (
        <span className="whitespace-nowrap text-xs text-muted-foreground">
          {timeAgo(row.original.skill.updated_at)}
        </span>
      ),
    },
    {
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
  const { t } = useT("skills");
  const { skill, canEdit } = row;
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
              {t(($) => $.table.lock_tooltip)}
            </TooltipContent>
          </Tooltip>
        )}
        <span className="inline-flex shrink-0 items-center gap-0.5 font-mono text-xs text-muted-foreground/70">
          <FileText className="h-3 w-3" />
          {totalFileCount(skill)}
        </span>
      </div>
      <div
        className={`mt-0.5 max-w-xl truncate text-xs ${
          skill.description
            ? "text-muted-foreground"
            : "italic text-muted-foreground/50"
        }`}
      >
        {skill.description || t(($) => $.table.no_description)}
      </div>
    </div>
  );
}

function AgentAssignees({ agents }: { agents: Agent[] }) {
  const { t } = useT("skills");
  if (agents.length === 0) {
    return <span className="text-xs text-muted-foreground/70">{t(($) => $.table.unused)}</span>;
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
  const { t } = useT("skills");
  const origin = readOrigin(skill);

  let icon = <Pencil className="h-3 w-3 shrink-0" />;
  let label: string = t(($) => $.table.source_manual);
  if (origin.type === "runtime_local") {
    icon = <HardDrive className="h-3 w-3 shrink-0" />;
    label = runtime
      ? t(($) => $.table.source_runtime_named, { name: runtime.name })
      : origin.provider
        ? t(($) => $.table.source_runtime_provider, { provider: origin.provider })
        : t(($) => $.table.source_runtime_unknown);
  } else if (origin.type === "clawhub") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = t(($) => $.table.source_clawhub);
  } else if (origin.type === "skills_sh") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = t(($) => $.table.source_skills_sh);
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
          <span className="truncate">{t(($) => $.table.by_creator, { name: creator.name })}</span>
        </div>
      )}
    </div>
  );
}
