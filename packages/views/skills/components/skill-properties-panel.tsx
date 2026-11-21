"use client";

import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Download, HardDrive, History, Pencil } from "@multica/ui/icons";
import type { AgentRuntime, MemberWithUser, Skill } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { resourceLabelsOptions } from "@multica/core/labels";
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
import { LabelChip } from "../../labels/label-chip";
import { LabelPicker } from "../../issues/components/pickers/label-picker";
import { useT, useTimeAgo } from "../../i18n";
import { originSourceUrl, totalFileCount, type OriginInfo } from "../lib/origin";

function ReadOnlySkillLabels({ skillId }: { skillId: string }) {
  const wsId = useWorkspaceId();
  const { data: labels = [] } = useQuery(resourceLabelsOptions(wsId, "skill", skillId));
  if (labels.length === 0) return <span className="text-muted-foreground">—</span>;
  return (
    <div className="flex min-w-0 flex-wrap gap-1 py-1.5">
      {labels.map((label) => (
        <LabelChip key={label.id} label={label} />
      ))}
    </div>
  );
}

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
 * A panel value that opens the version history, so the row reads as a link.
 * The accessible name keeps the visible value and says where it leads.
 */
function HistoryLinkValue({
  label,
  value,
  muted = false,
  mono = false,
  icon,
  onClick,
}: {
  label: string;
  value: string;
  muted?: boolean;
  mono?: boolean;
  icon?: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={`${label}: ${value}`}
      title={label}
      className="group/history flex min-w-0 flex-1 items-center gap-1.5 rounded-sm text-left transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
    >
      {icon}
      <span
        className={cn(
          "truncate",
          muted && "text-muted-foreground",
          mono && "font-mono tabular-nums",
        )}
      >
        {value}
      </span>
      <ChevronRight
        className="ml-auto size-3.5 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/history:opacity-100 group-focus-visible/history:opacity-100"
        aria-hidden="true"
      />
    </button>
  );
}

/**
 * The skill's properties. Name and description edit the page's draft, so
 * they save with the files through the same Save pill; labels save on pick.
 *
 * `latestVersion` is the current version number, `null` when the skill has no
 * history yet (it predates versioning and has not changed since), and
 * `undefined` while the history is loading or failed to load.
 */
export function SkillPropertiesPanel({
  skill,
  name,
  description,
  canEdit,
  origin,
  originRuntime,
  creator,
  latestVersion,
  onNameChange,
  onDescriptionChange,
  onOpenVersions,
}: {
  skill: Skill;
  name: string;
  description: string;
  canEdit: boolean;
  origin: OriginInfo;
  originRuntime: AgentRuntime | null;
  creator: MemberWithUser | null;
  latestVersion: number | null | undefined;
  onNameChange: (value: string) => void;
  onDescriptionChange: (value: string) => void;
  onOpenVersions: () => void;
}) {
  const { t } = useT("skills");
  const timeAgo = useTimeAgo();
  const historyLabel = t(($) => $.detail.menu.version_history);

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
        <ObjectProperty label={t(($) => $.detail.panel.version)}>
          <HistoryLinkValue
            label={historyLabel}
            icon={
              <History className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
            }
            value={
              latestVersion === undefined
                ? "—"
                : latestVersion === null
                  ? t(($) => $.detail.panel.no_versions)
                  : t(($) => $.detail.versions.version_label, { version: latestVersion })
            }
            muted={latestVersion == null}
            mono={typeof latestVersion === "number"}
            onClick={onOpenVersions}
          />
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.table.updated)}>
          {/* "Who changed what just now" is the usual reason to open history. */}
          <HistoryLinkValue
            label={historyLabel}
            value={timeAgo(skill.updated_at)}
            onClick={onOpenVersions}
          />
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.table.created)}>
          {timeAgo(skill.created_at)}
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.sections.files)}>
          {t(($) => $.detail.header.files, { count: totalFileCount(skill) })}
        </ObjectProperty>
        <ObjectProperty label={t(($) => $.detail.overview.labels)}>
          {canEdit ? (
            <LabelPicker skillId={skill.id} align="start" />
          ) : (
            <ReadOnlySkillLabels skillId={skill.id} />
          )}
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
