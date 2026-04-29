"use client";

import { Download, FileText, HardDrive, Lock, Pencil } from "lucide-react";
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
  name: 320,
  usedBy: 140,
  source: 240,
  updated: 100,
} as const;

export function createSkillColumns(): ColumnDef<SkillRow>[] {
  return [
    {
      id: "name",
      header: "Name",
      size: COL_WIDTHS.name,
      cell: ({ row }) => <SkillNameCell row={row.original} />,
    },
    {
      id: "usedBy",
      header: "Used by",
      size: COL_WIDTHS.usedBy,
      cell: ({ row }) => <AgentAssignees agents={row.original.agents} />,
    },
    {
      id: "source",
      header: "Source · Added by",
      size: COL_WIDTHS.source,
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
      header: "Updated",
      size: COL_WIDTHS.updated,
      cell: ({ row }) => (
        <span className="whitespace-nowrap text-xs text-muted-foreground">
          {timeAgo(row.original.skill.updated_at)}
        </span>
      ),
    },
  ];
}

// ---------------------------------------------------------------------------
// Cell renderers
// ---------------------------------------------------------------------------

function SkillNameCell({ row }: { row: SkillRow }) {
  const { skill, canEdit } = row;
  return (
    <div className="min-w-0">
      <div className="flex items-center gap-2">
        <span className="truncate font-medium">{skill.name}</span>
        {!canEdit && (
          <Tooltip>
            <TooltipTrigger
              render={
                <Lock className="h-3 w-3 shrink-0 text-muted-foreground/60" />
              }
            />
            <TooltipContent>
              Read-only — only creator or admin can edit
            </TooltipContent>
          </Tooltip>
        )}
        <span className="inline-flex shrink-0 items-center gap-0.5 font-mono text-xs text-muted-foreground/70">
          <FileText className="h-3 w-3" />
          {totalFileCount(skill)}
        </span>
      </div>
      <div
        className={`mt-0.5 line-clamp-1 text-xs ${
          skill.description
            ? "text-muted-foreground"
            : "italic text-muted-foreground/50"
        }`}
      >
        {skill.description || "No description"}
      </div>
    </div>
  );
}

function AgentAssignees({ agents }: { agents: Agent[] }) {
  if (agents.length === 0) {
    return <span className="text-xs text-muted-foreground/70">— unused</span>;
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
  const origin = readOrigin(skill);

  let icon = <Pencil className="h-3 w-3 shrink-0" />;
  let label = "Created manually";
  if (origin.type === "runtime_local") {
    icon = <HardDrive className="h-3 w-3 shrink-0" />;
    label = runtime
      ? `From ${runtime.name}`
      : origin.provider
        ? `From ${origin.provider} runtime`
        : "From a runtime";
  } else if (origin.type === "clawhub") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = "From ClawHub";
  } else if (origin.type === "skills_sh") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = "From Skills.sh";
  }

  return (
    <div className="min-w-0">
      <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
        <span className="shrink-0">{icon}</span>
        <span className="truncate">{label}</span>
      </div>
      {creator && (
        <div className="mt-1 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <ActorAvatar
            name={creator.name}
            initials={creator.name.slice(0, 2).toUpperCase()}
            avatarUrl={creator.avatar_url}
            size={14}
          />
          <span className="truncate">by {creator.name}</span>
        </div>
      )}
    </div>
  );
}
