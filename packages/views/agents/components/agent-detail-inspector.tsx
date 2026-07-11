"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
} from "@multica/core/types";
import { AGENT_DESCRIPTION_MAX_LENGTH } from "@multica/core/agents";
import { isImeComposing } from "@multica/core/utils";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { ActorAvatar } from "../../common/actor-avatar";
import { AvatarUploadControl } from "../../common/avatar-upload-control";
import { useAutoSave } from "../../settings/components/use-auto-save";
import { useT, useTimeAgo } from "../../i18n";
import { CharCounter } from "./char-counter";
import { AccessPicker } from "./inspector/access-picker";
import { ModelPicker } from "./inspector/model-picker";
import { RuntimePicker } from "./inspector/runtime-picker";
import { ThinkingSettingField } from "./inspector/thinking-prop-row";

interface InspectorProps {
  agent: Agent;
  runtime: AgentRuntime | null;
  owner: MemberWithUser | null;
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  currentUserId: string | null;
  canEdit: boolean;
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
}

interface ProfileDraft {
  name: string;
  description: string;
}

function profileDraftsEqual(left: ProfileDraft, right: ProfileDraft) {
  return left.name === right.name && left.description === right.description;
}

/**
 * Full-width General settings form. Every editable value is presented as an
 * explicit field; compact inspector chips are used only through their
 * settings-field variants, where the whole control is a visible click target.
 */
export function AgentDetailInspector({
  agent,
  runtime,
  owner,
  runtimes,
  members,
  currentUserId,
  canEdit,
  onUpdate,
}: InspectorProps) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const update = useCallback(
    (data: Record<string, unknown>) => onUpdate(agent.id, data),
    [agent.id, onUpdate],
  );

  const [name, setName] = useState(agent.name);
  const [description, setDescription] = useState(agent.description ?? "");

  useEffect(() => {
    setName(agent.name);
    setDescription(agent.description ?? "");
    // Reset only when moving to another agent. Cache updates from this form
    // must not erase a newer local draft while an autosave is in flight.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [agent.id]);

  const profileDraft = useMemo(
    () => ({ name: name.trim(), description }),
    [description, name],
  );
  const savedProfile = useMemo(
    () => ({
      name: agent.name,
      description: agent.description ?? "",
    }),
    [agent.description, agent.name],
  );
  const saveProfile = useCallback(
    async (next: ProfileDraft) => {
      await update({ name: next.name, description: next.description });
    },
    [update],
  );
  const profileAutoSave = useAutoSave({
    value: profileDraft,
    savedValue: savedProfile,
    onSave: saveProfile,
    enabled:
      canEdit &&
      profileDraft.name.length > 0 &&
      profileDraft.description.length <= AGENT_DESCRIPTION_MAX_LENGTH,
    isEqual: profileDraftsEqual,
  });

  const isOnline = runtime?.status === "online";
  const nameInvalid = name.trim().length === 0;

  return (
    <div className="space-y-10">
      <SettingsSection
        title={t(($) => $.inspector.section_profile)}
        description={t(($) => $.inspector.section_profile_hint)}
      >
        <div className="divide-y divide-surface-border border-y border-surface-border">
          <div className="flex flex-col gap-4 py-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div className="text-sm font-medium">
                {t(($) => $.inspector.avatar_label)}
              </div>
              <p className="mt-0.5 text-xs leading-5 text-muted-foreground">
                {t(($) => $.inspector.avatar_hint)}
              </p>
            </div>
            <AvatarUploadControl
              variant="agent"
              value={agent.avatar_url ?? null}
              name={agent.name}
              size={56}
              disabled={!canEdit}
              onUploaded={(url) => update({ avatar_url: url })}
            />
          </div>

          <div className="grid gap-5 py-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor={`agent-name-${agent.id}`}>
                {t(($) => $.inspector.name_label)}
              </Label>
              <Input
                id={`agent-name-${agent.id}`}
                type="text"
                name="agent-name"
                autoComplete="off"
                value={name}
                onChange={(event) => setName(event.target.value)}
                onBlur={profileAutoSave.flush}
                disabled={!canEdit}
                aria-invalid={nameInvalid || undefined}
              />
              {nameInvalid ? (
                <p className="text-xs text-destructive">
                  {t(($) => $.inspector.rename_required)}
                </p>
              ) : null}
            </div>

            <div className="space-y-2 sm:row-span-2">
              <Label htmlFor={`agent-description-${agent.id}`}>
                {t(($) => $.inspector.description_label)}
              </Label>
              <Textarea
                id={`agent-description-${agent.id}`}
                name="agent-description"
                autoComplete="off"
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                onBlur={profileAutoSave.flush}
                disabled={!canEdit}
                rows={5}
                maxLength={AGENT_DESCRIPTION_MAX_LENGTH}
                className="resize-y"
                placeholder={t(($) => $.inspector.description_placeholder)}
              />
              <CharCounter
                length={[...description].length}
                max={AGENT_DESCRIPTION_MAX_LENGTH}
              />
            </div>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.inspector.section_execution)}
        description={t(($) => $.inspector.section_execution_hint)}
      >
        <div className="grid gap-5 border-y border-surface-border py-4 sm:grid-cols-2">
          <RuntimePicker
            variant="field"
            value={agent.runtime_id}
            runtimes={runtimes}
            members={members}
            currentUserId={currentUserId}
            canEdit={canEdit}
            onChange={(id) => update({ runtime_id: id })}
          />
          <ModelPicker
            variant="field"
            runtimeId={agent.runtime_id}
            runtimeOnline={!!isOnline}
            value={agent.model ?? ""}
            canEdit={canEdit}
            onChange={(model) => update({ model })}
          />
          <ThinkingSettingField
            runtimeId={agent.runtime_id}
            runtimeOnline={!!isOnline}
            provider={runtime?.provider ?? ""}
            model={agent.model ?? ""}
            value={agent.thinking_level ?? ""}
            canEdit={canEdit}
            onChange={(thinkingLevel) =>
              update({ thinking_level: thinkingLevel })
            }
          />
          <ConcurrencyField
            value={agent.max_concurrent_tasks}
            canEdit={canEdit}
            onSave={(next) => update({ max_concurrent_tasks: next })}
          />
        </div>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.inspector.section_access)}
        description={t(($) => $.inspector.section_access_hint)}
      >
        <AccessPicker
          permissionMode={agent.permission_mode}
          invocationTargets={agent.invocation_targets}
          visibility={agent.visibility}
          members={members}
          ownerId={agent.owner_id}
          canEdit={
            currentUserId !== null && agent.owner_id === currentUserId
          }
          hasComposioAllowlist={
            (agent.composio_toolkit_allowlist ?? []).length > 0
          }
          onChange={(next) => update(next)}
        />
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.inspector.section_details)}
        description={t(($) => $.inspector.section_details_hint)}
      >
        <dl className="divide-y divide-surface-border border-y border-surface-border">
          {owner ? (
            <MetadataRow label={t(($) => $.inspector.prop_owner)}>
              <span className="flex min-w-0 items-center gap-2">
                <ActorAvatar
                  actorType="member"
                  actorId={owner.user_id}
                  size="xs"
                />
                <span className="truncate">{owner.name}</span>
              </span>
            </MetadataRow>
          ) : null}
          <MetadataRow label={t(($) => $.inspector.prop_created)}>
            {timeAgo(agent.created_at)}
          </MetadataRow>
          <MetadataRow label={t(($) => $.inspector.prop_updated)}>
            {timeAgo(agent.updated_at)}
          </MetadataRow>
        </dl>
      </SettingsSection>
    </div>
  );
}

function SettingsSection({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div className="px-0.5">
        <h3 className="text-sm font-medium text-balance">{title}</h3>
        <p className="mt-1 max-w-2xl text-pretty text-xs leading-5 text-muted-foreground">
          {description}
        </p>
      </div>
      {children}
    </section>
  );
}

function ConcurrencyField({
  value,
  canEdit,
  onSave,
}: {
  value: number;
  canEdit: boolean;
  onSave: (next: number) => Promise<void>;
}) {
  const { t } = useT("agents");
  const [draft, setDraft] = useState(String(value));
  const min = 1;
  const max = 50;

  useEffect(() => setDraft(String(value)), [value]);

  const commit = () => {
    const next = Number(draft);
    if (!Number.isInteger(next) || next < min || next > max) {
      setDraft(String(value));
      return;
    }
    if (next !== value) void onSave(next);
  };

  return (
    <div className="flex min-w-0 flex-col">
      <Label htmlFor="agent-concurrency">
        {t(($) => $.inspector.prop_concurrency)}
      </Label>
      <Input
        id="agent-concurrency"
        type="number"
        name="agent-concurrency"
        autoComplete="off"
        inputMode="numeric"
        min={min}
        max={max}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={commit}
        onKeyDown={(event) => {
          if (isImeComposing(event)) return;
          if (event.key === "Enter") {
            event.preventDefault();
            commit();
          }
        }}
        disabled={!canEdit}
        className="mt-1.5 font-mono tabular-nums"
      />
      <p className="mt-1 text-xs text-muted-foreground">
        {t(($) => $.pickers.concurrency_range, { min, max })}
      </p>
    </div>
  );
}

function MetadataRow({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="grid min-h-12 grid-cols-[9rem_minmax(0,1fr)] items-center gap-4 py-3 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-foreground">{children}</dd>
    </div>
  );
}
