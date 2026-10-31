"use client";

import { Download, HardDrive, Pencil } from "@multica/ui/icons";
import type { AgentRuntime, MemberWithUser, Skill } from "@multica/core/types";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { ObjectPanelSection, ObjectProperty } from "../../layout/object-page";
import { ResourceLabelPicker } from "../../labels/resource-label-picker";
import { useT, useTimeAgo } from "../../i18n";
import { originSourceUrl, totalFileCount, type OriginInfo } from "../lib/origin";

function useOriginLabel(origin: OriginInfo, runtime: AgentRuntime | null) {
  const { t } = useT("skills");
  if (origin.type === "runtime_local") {
    return runtime
      ? t(($) => $.detail.subline.origin_runtime_named, {
          name: runtimeDisplayLabel(runtime),
        })
      : origin.provider
        ? t(($) => $.detail.subline.origin_runtime_provider, { provider: origin.provider })
        : t(($) => $.detail.subline.origin_runtime_unknown);
  }
  if (origin.type === "clawhub") return t(($) => $.detail.subline.origin_clawhub);
  if (origin.type === "skills_sh") return t(($) => $.detail.subline.origin_skills_sh);
  if (origin.type === "github") return t(($) => $.detail.subline.origin_github);
  return t(($) => $.detail.subline.origin_workspace);
}

function SourceValue({
  origin,
  runtime,
}: {
  origin: OriginInfo;
  runtime: AgentRuntime | null;
}) {
  const label = useOriginLabel(origin, runtime);
  const sourceUrl = originSourceUrl(origin);
  // Same three-way split as the list's Source column: runtime, created here,
  // imported.
  const Icon =
    origin.type === "runtime_local"
      ? HardDrive
      : origin.type === "manual"
        ? Pencil
        : Download;
  return (
    <>
      <Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
      {sourceUrl ? (
        <Tooltip>
          <TooltipTrigger
            render={
              <a
                href={sourceUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="truncate hover:underline"
              >
                {label}
              </a>
            }
          />
          <TooltipContent side="top">{sourceUrl}</TooltipContent>
        </Tooltip>
      ) : (
        <span className="truncate">{label}</span>
      )}
    </>
  );
}

/**
 * Description opens into a field sized for it: real descriptions run 500–900
 * characters (they carry the trigger vocabulary an agent matches on), far
 * more than a panel row can show.
 */
function DescriptionValue({
  description,
  canEdit,
  onChange,
}: {
  description: string;
  canEdit: boolean;
  onChange: (value: string) => void;
}) {
  const { t } = useT("skills");
  return (
    <Popover>
      <PopoverTrigger
        render={
          <button
            type="button"
            className={cn(
              "min-w-0 flex-1 truncate text-left transition-colors hover:text-foreground",
              !description && "text-muted-foreground",
            )}
          >
            {description ||
              (canEdit ? t(($) => $.detail.panel.add_description) : "—")}
          </button>
        }
      />
      <PopoverContent side="left" align="start" className="w-96 p-3">
        <Textarea
          id="skill-description"
          aria-label={t(($) => $.detail.overview.description)}
          value={description}
          readOnly={!canEdit}
          onChange={(e) => onChange(e.target.value)}
          placeholder={t(($) => $.detail.description_placeholder)}
          rows={8}
          className="text-body leading-relaxed read-only:cursor-default"
        />
        <div className="mt-1.5 flex flex-wrap justify-between gap-x-4 gap-y-1 text-caption text-muted-foreground">
          <p>{t(($) => $.detail.overview.description_hint)}</p>
          <span className="tabular-nums">
            {t(($) => $.detail.overview.character_count, {
              count: description.length,
            })}
          </span>
        </div>
      </PopoverContent>
    </Popover>
  );
}

/**
 * The skill's properties. Name and description edit the page's draft, so
 * they save with the files through the same Save pill; labels save on pick.
 */
export function SkillPropertiesPanel({
  skill,
  name,
  description,
  canEdit,
  origin,
  originRuntime,
  creator,
  onNameChange,
  onDescriptionChange,
}: {
  skill: Skill;
  name: string;
  description: string;
  canEdit: boolean;
  origin: OriginInfo;
  originRuntime: AgentRuntime | null;
  creator: MemberWithUser | null;
  onNameChange: (value: string) => void;
  onDescriptionChange: (value: string) => void;
}) {
  const { t } = useT("skills");
  const timeAgo = useTimeAgo();

  return (
    <div className="space-y-5">
      <ObjectPanelSection title={t(($) => $.detail.overview.properties)}>
        <ObjectProperty label={t(($) => $.detail.overview.name)}>
          {canEdit ? (
            <input
              id="skill-name"
              aria-label={t(($) => $.detail.overview.name)}
              value={name}
              onChange={(e) => onNameChange(e.target.value)}
              placeholder={t(($) => $.detail.name_placeholder)}
              className="h-7 min-w-0 flex-1 rounded-sm bg-transparent font-mono text-caption outline-none placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring"
            />
          ) : (
            <span className="truncate font-mono">{name}</span>
          )}
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.overview.description)}>
          <DescriptionValue
            description={description}
            canEdit={canEdit}
            onChange={onDescriptionChange}
          />
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.overview.labels)}>
          <ResourceLabelPicker
            resourceType="skill"
            resourceId={skill.id}
            canEdit={canEdit}
          />
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.table.source)}>
          <SourceValue origin={origin} runtime={originRuntime} />
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.table.created_by)}>
          {creator ? (
            <>
              <ActorAvatar
                name={creator.name}
                initials={creator.name.slice(0, 2).toUpperCase()}
                avatarUrl={resolvePublicFileUrl(creator.avatar_url)}
                size="sm"
              />
              <span className="truncate">{creator.name}</span>
            </>
          ) : (
            <span className="text-muted-foreground">—</span>
          )}
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.table.updated)}>
          {timeAgo(skill.updated_at)}
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.table.created)}>
          {timeAgo(skill.created_at)}
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.sections.files)}>
          {t(($) => $.detail.header.files, { count: totalFileCount(skill) })}
        </ObjectProperty>
      </ObjectPanelSection>

      <p className="px-2 text-caption leading-relaxed text-muted-foreground">
        {canEdit
          ? t(($) => $.detail.overview.permissions_owner)
          : creator
            ? t(($) => $.detail.overview.permissions_locked_creator, {
                name: creator.name,
              })
            : t(($) => $.detail.overview.permissions_locked)}
      </p>
    </div>
  );
}
