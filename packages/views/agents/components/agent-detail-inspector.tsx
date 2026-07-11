"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
} from "@multica/core/types";
import { AGENT_DESCRIPTION_MAX_LENGTH } from "@multica/core/agents";
import { isImeComposing } from "@multica/core/utils";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { AvatarUploadControl } from "../../common/avatar-upload-control";
import {
  SettingsCard,
  SettingsRow,
  SettingsSaveState,
  SettingsSection,
} from "../../settings/components/settings-layout";
import { useAutoSave } from "../../settings/components/use-auto-save";
import { useT } from "../../i18n";
import { CharCounter } from "./char-counter";
import { ModelPicker } from "./inspector/model-picker";
import { RuntimePicker } from "./inspector/runtime-picker";
import { ThinkingSettingField } from "./inspector/thinking-prop-row";

interface InspectorProps {
  agent: Agent;
  runtime: AgentRuntime | null;
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
  runtimes,
  members,
  currentUserId,
  canEdit,
  onUpdate,
}: InspectorProps) {
  const { t } = useT("agents");
  const { t: ts } = useT("settings");
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
    <div className="space-y-8">
      <SettingsSection
        title={t(($) => $.inspector.section_profile)}
        description={t(($) => $.inspector.section_profile_hint)}
        action={
          <SettingsSaveState
            status={profileAutoSave.status}
            savingLabel={ts(($) => $.auto_save.saving)}
            savedLabel={ts(($) => $.auto_save.saved)}
            errorLabel={ts(($) => $.auto_save.failed)}
          />
        }
      >
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.inspector.avatar_label)}
            description={t(($) => $.inspector.avatar_hint)}
            controlClassName="sm:max-w-none"
          >
            <div className="flex justify-start sm:justify-end">
              <AvatarUploadControl
                variant="agent"
                value={agent.avatar_url ?? null}
                name={agent.name}
                size={56}
                disabled={!canEdit}
                onUploaded={(url) => update({ avatar_url: url })}
              />
            </div>
          </SettingsRow>

          <SettingsRow
            label={t(($) => $.inspector.name_label)}
            controlClassName="sm:w-80"
          >
            <div>
              <Input
                type="text"
                name="agent-name"
                autoComplete="off"
                aria-label={t(($) => $.inspector.name_label)}
                value={name}
                onChange={(event) => setName(event.target.value)}
                onBlur={profileAutoSave.flush}
                disabled={!canEdit}
                aria-invalid={nameInvalid || undefined}
              />
              {nameInvalid ? (
                <p className="mt-1 text-xs text-destructive">
                  {t(($) => $.inspector.rename_required)}
                </p>
              ) : null}
            </div>
          </SettingsRow>

          <SettingsRow
            label={t(($) => $.inspector.description_label)}
            controlClassName="sm:w-96"
            align="start"
          >
            <div>
              <Textarea
                name="agent-description"
                autoComplete="off"
                aria-label={t(($) => $.inspector.description_label)}
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
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>

      <SettingsSection
        title={t(($) => $.inspector.section_execution)}
        description={t(($) => $.inspector.section_execution_hint)}
      >
        <SettingsCard>
          <SettingsRow
            label={t(($) => $.inspector.prop_runtime)}
            controlClassName="sm:w-80"
          >
            <RuntimePicker
              variant="field"
              showLabel={false}
              value={agent.runtime_id}
              runtimes={runtimes}
              members={members}
              currentUserId={currentUserId}
              canEdit={canEdit}
              onChange={(id) => update({ runtime_id: id })}
            />
          </SettingsRow>
          <SettingsRow
            label={t(($) => $.inspector.prop_model)}
            controlClassName="sm:w-80"
          >
            <ModelPicker
              variant="field"
              showLabel={false}
              runtimeId={agent.runtime_id}
              runtimeOnline={!!isOnline}
              value={agent.model ?? ""}
              canEdit={canEdit}
              onChange={(model) => update({ model })}
            />
          </SettingsRow>
          <ThinkingSettingField
            label={t(($) => $.inspector.prop_thinking)}
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
          <SettingsRow
            label={t(($) => $.inspector.prop_concurrency)}
            controlClassName="sm:w-80"
          >
            <ConcurrencyField
              value={agent.max_concurrent_tasks}
              canEdit={canEdit}
              onSave={(next) => update({ max_concurrent_tasks: next })}
            />
          </SettingsRow>
        </SettingsCard>
      </SettingsSection>
    </div>
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
    <div>
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
        aria-label={t(($) => $.inspector.prop_concurrency)}
        className="font-mono tabular-nums"
      />
      <p className="mt-1 text-xs text-muted-foreground">
        {t(($) => $.pickers.concurrency_range, { min, max })}
      </p>
    </div>
  );
}
