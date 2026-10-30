"use client";

import { Clock3, Download, FileText, HardDrive, Pencil, UserPlus } from "@multica/ui/icons";
import type { Agent, AgentRuntime, MemberWithUser, Skill } from "@multica/core/types";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { runtimeDisplayLabel } from "@multica/core/runtimes";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@multica/ui/components/ui/tooltip";
import { useT, useTimeAgo } from "../../i18n";
import { ResourceLabelPicker } from "../../labels/resource-label-picker";
import { originSourceUrl, totalFileCount, type OriginInfo } from "../lib/origin";

function useOriginLabel(origin: OriginInfo | null, runtime: AgentRuntime | null) {
  const { t } = useT("skills");
  if (!origin) return null;
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

function Section({
  title,
  action,
  children,
}: {
  title: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="border-b border-surface-border px-4 py-3 last:border-b-0">
      <div className="mb-1.5 flex h-6 items-center gap-2">
        <h3 className="min-w-0 flex-1 truncate text-caption font-medium text-muted-foreground">
          {title}
        </h3>
        {action}
      </div>
      {children}
    </section>
  );
}

function Field({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="py-1.5">
      <label htmlFor={htmlFor} className="mb-1 block text-caption text-muted-foreground">
        {label}
      </label>
      {children}
    </div>
  );
}

function Fact({ icon, children }: { icon: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex min-w-0 items-center gap-2 py-1 text-caption text-muted-foreground">
      <span className="flex size-3.5 shrink-0 items-center justify-center [&>svg]:size-3.5">
        {icon}
      </span>
      <span className="min-w-0 truncate">{children}</span>
    </div>
  );
}

/**
 * Everything about the skill that is not a file: where it came from, its
 * agent-facing name and description, and who uses it. Sits beside the open
 * file, the same way a doc's properties sit beside the doc.
 */
export function SkillSidePanel({
  skill,
  origin,
  originRuntime,
  creator,
  name,
  description,
  canEdit,
  skillAgents,
  onNameChange,
  onDescriptionChange,
  onAddToAgents,
}: {
  skill: Skill;
  origin: OriginInfo | null;
  originRuntime: AgentRuntime | null;
  creator: MemberWithUser | null;
  name: string;
  description: string;
  canEdit: boolean;
  skillAgents: Agent[];
  onNameChange: (value: string) => void;
  onDescriptionChange: (value: string) => void;
  onAddToAgents: () => void;
}) {
  const { t } = useT("skills");
  const timeAgo = useTimeAgo();
  const originLabel = useOriginLabel(origin, originRuntime);
  const sourceUrl = originSourceUrl(origin);

  return (
    <div className="flex h-full flex-col overflow-y-auto">
      <Section title={t(($) => $.detail.overview.properties)}>
        {originLabel && (
          <Fact
            icon={
              origin?.type === "runtime_local" ? (
                <HardDrive aria-hidden="true" />
              ) : origin?.type === "manual" ? (
                <Pencil aria-hidden="true" />
              ) : (
                <Download aria-hidden="true" />
              )
            }
          >
            {sourceUrl ? (
              <Tooltip>
                <TooltipTrigger
                  render={
                    <a
                      href={sourceUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="hover:text-foreground hover:underline"
                    >
                      {originLabel}
                    </a>
                  }
                />
                <TooltipContent side="left">{sourceUrl}</TooltipContent>
              </Tooltip>
            ) : (
              originLabel
            )}
          </Fact>
        )}
        <Fact icon={<FileText aria-hidden="true" />}>
          {t(($) => $.detail.header.files, { count: totalFileCount(skill) })}
        </Fact>
        <Fact icon={<Clock3 aria-hidden="true" />}>
          {creator
            ? t(($) => $.detail.header.updated_by, {
                when: timeAgo(skill.updated_at),
                name: creator.name,
              })
            : t(($) => $.detail.header.updated, { when: timeAgo(skill.updated_at) })}
        </Fact>

        <div className="mt-2">
          <Field label={t(($) => $.detail.overview.name)} htmlFor="skill-name">
            <Input
              id="skill-name"
              value={name}
              readOnly={!canEdit}
              onChange={(e) => onNameChange(e.target.value)}
              placeholder={t(($) => $.detail.name_placeholder)}
              className="h-8 font-mono text-body read-only:cursor-default"
            />
          </Field>
          <Field label={t(($) => $.detail.overview.description)} htmlFor="skill-description">
            {/* Real descriptions run 500–900 characters (they carry the
                trigger vocabulary an agent matches on), so the field is sized
                for the data. */}
            <Textarea
              id="skill-description"
              value={description}
              readOnly={!canEdit}
              onChange={(e) => onDescriptionChange(e.target.value)}
              placeholder={t(($) => $.detail.description_placeholder)}
              rows={8}
              className="text-body leading-relaxed read-only:cursor-default"
            />
            <div className="mt-1 flex justify-between gap-3 text-caption text-faint-foreground">
              <p className="min-w-0">{t(($) => $.detail.overview.description_hint)}</p>
              <span className="shrink-0 tabular-nums">
                {t(($) => $.detail.overview.character_count, { count: description.length })}
              </span>
            </div>
          </Field>
          <Field label={t(($) => $.detail.overview.labels)}>
            <ResourceLabelPicker resourceType="skill" resourceId={skill.id} canEdit={canEdit} />
          </Field>
          <p className="pt-1.5 text-caption text-faint-foreground">
            {t(($) => $.detail.overview.properties_hint)}
          </p>
        </div>
      </Section>

      <Section
        title={t(($) => $.detail.overview.used_by, { count: skillAgents.length })}
        action={
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  aria-label={t(($) => $.actions.add_to_agent)}
                  onClick={onAddToAgents}
                  className="flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-surface-hover hover:text-foreground"
                />
              }
            >
              <UserPlus className="size-3.5" />
            </TooltipTrigger>
            <TooltipContent>{t(($) => $.actions.add_to_agent)}</TooltipContent>
          </Tooltip>
        }
      >
        {skillAgents.length === 0 ? (
          <p className="py-1 text-caption text-faint-foreground">
            {t(($) => $.detail.overview.used_by_empty)}
          </p>
        ) : (
          <ul className="space-y-px">
            {skillAgents.map((a) => (
              <li key={a.id} className="flex h-8 items-center gap-2 rounded-md px-1.5">
                <ActorAvatar
                  name={a.name}
                  initials={a.name.slice(0, 2).toUpperCase()}
                  avatarUrl={resolvePublicFileUrl(a.avatar_url)}
                  isAgent
                  size="xs"
                />
                <span className="min-w-0 flex-1 truncate text-body">{a.name}</span>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <p className="px-4 py-3 text-caption leading-relaxed text-faint-foreground">
        {canEdit
          ? t(($) => $.detail.overview.permissions_owner)
          : creator
            ? t(($) => $.detail.overview.permissions_locked_creator, { name: creator.name })
            : t(($) => $.detail.overview.permissions_locked)}
      </p>
    </div>
  );
}
