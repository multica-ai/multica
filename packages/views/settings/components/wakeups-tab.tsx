"use client";

import { useEffect, useId, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useUpdateWorkspaceSystemWakeup, workspaceSystemWakeupsOptions } from "@multica/core/issues";
import type { WorkspaceSystemWakeup } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow, SettingsSection, SettingsTab } from "./settings-layout";

const MAX_INSTRUCTION_BYTES = 4000;

/**
 * Workspace defaults of the platform's wakeup rules. Each issue can change
 * its own rule in its Wakeups section; those issues stop following these
 * defaults.
 */
export function WakeupsTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const { data: rules = [], isError } = useQuery(workspaceSystemWakeupsOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentUser = useAuthStore((s) => s.user);
  const canManage = useMemo(() => {
    const role = members.find((m) => m.user_id === currentUser?.id)?.role;
    return role === "owner" || role === "admin";
  }, [members, currentUser]);
  const childDone = rules.find((rule) => rule.rule === "child_done");
  return (
    <SettingsTab title={t(($) => $.page.tabs.wakeups)} description={t(($) => $.wakeups.description)}>
      {isError && (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.wakeups.load_error)}
        </p>
      )}
      {childDone && <ChildDoneDefault rule={childDone} canManage={canManage} />}
      {childDone && !canManage && <p className="text-caption text-muted-foreground">{t(($) => $.wakeups.admin_only)}</p>}
    </SettingsTab>
  );
}

function ChildDoneDefault({ rule, canManage }: { rule: WorkspaceSystemWakeup; canManage: boolean }) {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const update = useUpdateWorkspaceSystemWakeup(wsId);
  const id = useId();
  const [instruction, setInstruction] = useState(rule.instruction);
  const [error, setError] = useState("");
  // A save elsewhere (another tab, another admin) replaces an untouched draft.
  useEffect(() => setInstruction(rule.instruction), [rule.instruction]);
  const dirty = instruction.trim() !== rule.instruction;
  const save = async (input: { enabled?: boolean; instruction?: string }) => {
    try {
      await update.mutateAsync({ rule: rule.rule, ...input });
    } catch {
      toast.error(t(($) => $.wakeups.save_error));
      throw new Error("save failed");
    }
  };
  return (
    <SettingsSection title={t(($) => $.wakeups.child_done_title)} description={t(($) => $.wakeups.child_done_description)}>
      <SettingsCard>
        <SettingsRow
          label={<label htmlFor={`${id}-enabled`}>{t(($) => $.wakeups.enabled)}</label>}
          description={rule.customized > 0 ? t(($) => $.wakeups.customized, { count: rule.customized }) : undefined}
        >
          <Switch
            id={`${id}-enabled`}
            checked={rule.enabled}
            disabled={!canManage || update.isPending}
            aria-busy={update.isPending || undefined}
            onCheckedChange={(enabled) => void save({ enabled }).catch(() => undefined)}
          />
        </SettingsRow>
        <form
          className="space-y-2 px-4 py-3.5"
          onSubmit={(event) => {
            event.preventDefault();
            const next = instruction.trim();
            if (new TextEncoder().encode(next).length > MAX_INSTRUCTION_BYTES) {
              setError(t(($) => $.wakeups.instruction_invalid));
              return;
            }
            void save({ instruction: next }).catch(() => undefined);
          }}
        >
          <label htmlFor={`${id}-instruction`} className="text-body font-medium">
            {t(($) => $.wakeups.instruction)}
          </label>
          <p id={`${id}-hint`} className="text-caption leading-5 text-muted-foreground">
            {t(($) => $.wakeups.instruction_hint)}
          </p>
          <Textarea
            id={`${id}-instruction`}
            rows={3}
            value={instruction}
            placeholder={t(($) => $.wakeups.instruction_placeholder)}
            disabled={!canManage || update.isPending}
            aria-describedby={error ? `${id}-hint ${id}-error` : `${id}-hint`}
            aria-invalid={!!error}
            className="resize-y text-base md:text-body"
            onChange={(event) => {
              setInstruction(event.target.value);
              setError("");
            }}
            onKeyDown={(event) => {
              if ((event.metaKey || event.ctrlKey) && event.key === "Enter" && !event.nativeEvent.isComposing) {
                event.preventDefault();
                event.currentTarget.form?.requestSubmit();
              }
            }}
          />
          {error && (
            <p id={`${id}-error`} role="alert" className="text-caption text-destructive">
              {error}
            </p>
          )}
          {rule.builtin_instruction && (
            <div className="rounded-md bg-muted/60 px-3 py-2 text-caption leading-5 text-muted-foreground">
              <p className="font-medium">{t(($) => $.wakeups.builtin)}</p>
              <p className="mt-0.5 whitespace-pre-wrap break-words">{rule.builtin_instruction}</p>
            </div>
          )}
          {canManage && (
            <div className="flex justify-end">
              <Button type="submit" size="sm" disabled={!dirty || update.isPending}>
                {update.isPending ? t(($) => $.wakeups.saving) : t(($) => $.wakeups.save)}
              </Button>
            </div>
          )}
        </form>
      </SettingsCard>
    </SettingsSection>
  );
}
