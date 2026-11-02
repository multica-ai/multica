"use client";

import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { Workspace } from "@multica/core/types";
import { Switch } from "@multica/ui/components/ui/switch";
import { useT } from "../../i18n";
import { SettingsCard, SettingsRow } from "./settings-layout";

/** The server's defaults: catch-up on, from 10:00 local time (spec 2.9). */
const DEFAULT_ENABLED = true;
const DEFAULT_HOUR = 10;
const HOURS = Array.from({ length: 24 }, (_, hour) => hour);

/**
 * The daily brief's catch-up (spec 2.9): whether a missed brief is written later
 * in the day, and from which local hour. Both live in the workspace settings
 * object, which the server replaces as a whole, so every save sends every key the
 * workspace already has and changes only the one the member picked.
 */
export function BriefCatchupSetting({ workspace, canManage }: { workspace: Workspace; canManage: boolean }) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const [saving, setSaving] = useState(false);
  const settings = workspace.settings ?? {};
  const enabled = typeof settings.brief_catchup_enabled === "boolean" ? settings.brief_catchup_enabled : DEFAULT_ENABLED;
  const hour = readHour(settings.brief_catchup_hour);

  async function persist(patch: { brief_catchup_enabled?: boolean; brief_catchup_hour?: number }) {
    setSaving(true);
    try {
      const updated = await api.updateWorkspace(workspace.id, { settings: { ...settings, ...patch } });
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] | undefined) =>
        old?.map((ws) => (ws.id === updated.id ? updated : ws)),
      );
      toast.success(t(($) => $.workspace.brief_catchup_saved));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.workspace.brief_catchup_failed));
    } finally {
      setSaving(false);
    }
  }

  return (
    <SettingsCard>
      <SettingsRow
        anchor="brief-catchup"
        label={t(($) => $.workspace.brief_catchup_label)}
        description={t(($) => $.workspace.brief_catchup_description)}
      >
        <Switch
          checked={enabled}
          disabled={!canManage || saving}
          onCheckedChange={(checked) => persist({ brief_catchup_enabled: checked })}
          aria-label={t(($) => $.workspace.brief_catchup_label)}
        />
      </SettingsRow>
      <SettingsRow anchor="brief-catchup-hour" label={t(($) => $.workspace.brief_catchup_hour_label)}>
        <select
          value={hour}
          disabled={!canManage || saving || !enabled}
          aria-label={t(($) => $.workspace.brief_catchup_hour_label)}
          onChange={(event) => persist({ brief_catchup_hour: Number(event.target.value) })}
          className="h-9 rounded-lg border border-input bg-background px-3 text-body text-foreground focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50"
        >
          {HOURS.map((option) => (
            <option key={option} value={option}>
              {t(($) => $.workspace.brief_catchup_hour_option, { hour: String(option).padStart(2, "0") })}
            </option>
          ))}
        </select>
      </SettingsRow>
    </SettingsCard>
  );
}

/** A stored hour from 0 to 23, or the default when the workspace has none that is valid. */
function readHour(value: unknown): number {
  return typeof value === "number" && Number.isInteger(value) && value >= 0 && value <= 23 ? value : DEFAULT_HOUR;
}
