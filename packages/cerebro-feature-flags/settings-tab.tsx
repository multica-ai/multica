"use client";

import type { ComponentType } from "react";
import { Lock } from "lucide-react";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";
import { Label } from "@multica/ui/components/ui/label";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { WakeupLimitsSettings } from "@multica/cerebro-wakeup";
import { CEREBRO_FLAG_GROUPS, flagsForGroup, type CerebroFlagKey } from "./registry";
import { SecretaryCriteriaSettings } from "./secretary-criteria-settings";
import {
  useFeatureFlag,
  useFeatureFlagsQuery,
  useSetFeatureFlagMutation,
  useSetWorkspaceFeatureFlagMutation,
} from "./api";
import { useFlagLocked, useWorkspaceFlagValue } from "./store";

// Per-feature inline settings. A cerebro feature can attach extra controls that
// render under its toggle, so everything for that feature lives in one place in
// the Cerebro features tab. Add a feature's settings component here keyed by its
// flag; the row renders it whenever the feature is enabled. TECH-3298.
const FLAG_SETTINGS: Partial<Record<CerebroFlagKey, ComponentType>> = {
  cerebro_wakeup_time: WakeupLimitsSettings,
  cerebro_inbox_secretary: SecretaryCriteriaSettings,
};

// Exported so a feature can render its own toggles in a dedicated settings tab
// (e.g. the Notes tab, TECH-3637) with the same personal/workspace 3-way
// control instead of duplicating the logic.
export function FlagRow({
  flagKey,
  label,
  description,
  isAdmin,
}: {
  flagKey: CerebroFlagKey;
  label: string;
  description: string;
  isAdmin: boolean;
}) {
  const enabled = useFeatureFlag(flagKey);
  const locked = useFlagLocked(flagKey);
  const workspaceValue = useWorkspaceFlagValue(flagKey);
  const personalMutation = useSetFeatureFlagMutation();
  const workspaceMutation = useSetWorkspaceFeatureFlagMutation();

  // The owner's workspace-level setting, as a 3-way mode:
  //   "default" — no workspace override; members fall back to personal/default.
  //   "on"      — forced on for everyone (locked).
  //   "off"     — forced off for everyone (locked); this is the reliable
  //               org-wide kill switch that personal toggles can't restore.
  const teamMode: "default" | "on" | "off" = !locked
    ? "default"
    : workspaceValue === false
      ? "off"
      : "on";

  // Optional inline settings for this feature, shown only while it's enabled.
  const SettingsComponent = FLAG_SETTINGS[flagKey];

  const handlePersonalChange = (next: boolean) => {
    personalMutation.mutate(
      { key: flagKey, enabled: next },
      {
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : `Failed to update ${label}`);
        },
      },
    );
  };

  const handleTeamModeChange = (mode: "default" | "on" | "off") => {
    // default => clear the workspace override (members choose).
    // on/off  => force that value for everyone and lock it so a personal
    //            toggle can't override it.
    workspaceMutation.mutate(
      {
        key: flagKey,
        enabled: mode === "default" ? null : mode === "on",
        locked: mode !== "default",
      },
      {
        onError: (err) => {
          toast.error(err instanceof Error ? err.message : `Failed to update ${label}`);
        },
      },
    );
  };

  return (
    <Card>
      <CardContent>
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <Label className="text-sm font-medium">{label}</Label>
            <p className="text-xs text-muted-foreground">{description}</p>
            {locked && (
              <p className="flex items-center gap-1 text-xs text-muted-foreground">
                <Lock className="h-3 w-3" />
                {isAdmin
                  ? teamMode === "off"
                    ? "Forced off for the whole workspace."
                    : "Forced on for the whole workspace."
                  : teamMode === "off"
                    ? "Turned off for the whole workspace by an owner — you can't change this."
                    : "Turned on for the whole workspace by an owner — you can't change this."}
              </p>
            )}
          </div>
          <Switch
            checked={enabled}
            disabled={personalMutation.isPending || locked}
            onCheckedChange={handlePersonalChange}
            aria-label={label}
          />
        </div>

        {isAdmin && (
          <div className="mt-3 flex items-center justify-between gap-4 border-t pt-3">
            <Label className="text-xs font-medium text-muted-foreground">
              Whole workspace
            </Label>
            <NativeSelect
              size="sm"
              value={teamMode}
              disabled={workspaceMutation.isPending}
              onChange={(e) =>
                handleTeamModeChange(e.target.value as "default" | "on" | "off")
              }
              aria-label={`${label} — whole-workspace setting`}
            >
              <NativeSelectOption value="default">Members choose</NativeSelectOption>
              <NativeSelectOption value="on">Forced on for everyone</NativeSelectOption>
              <NativeSelectOption value="off">Forced off for everyone</NativeSelectOption>
            </NativeSelect>
          </div>
        )}

        {SettingsComponent && enabled && (
          <div className="mt-3 border-t pt-3">
            <SettingsComponent />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export function CerebroFeatureFlagsTab() {
  const query = useFeatureFlagsQuery();
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-sm font-semibold">Cerebro features</h2>
        <p className="text-xs text-muted-foreground">
          Toggle cerebro-fork features for this workspace. Defaults are on; turning
          a feature off falls back to upstream multica behaviour where applicable.
          Workspace owners can force a feature on for everyone so it can&apos;t be turned off.
        </p>
      </div>

      {query.isError && (
        <p className="text-xs text-destructive">
          Failed to load feature flags: {query.error instanceof Error ? query.error.message : "unknown error"}
        </p>
      )}

      <div className="space-y-6">
        {CEREBRO_FLAG_GROUPS.map((group) => {
          const flags = flagsForGroup(group.key);
          if (flags.length === 0) return null;
          return (
            <div key={group.key} className="space-y-2">
              <div className="space-y-0.5">
                <h3 className="text-sm font-semibold">{group.label}</h3>
                <p className="text-xs text-muted-foreground">{group.description}</p>
              </div>
              <div className="space-y-3">
                {flags.map((flag) => (
                  <FlagRow
                    key={flag.key}
                    flagKey={flag.key}
                    label={flag.label}
                    description={flag.description}
                    isAdmin={isAdmin}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}
