"use client";

import { Lock } from "lucide-react";
import { Switch } from "@multica/ui/components/ui/switch";
import { Label } from "@multica/ui/components/ui/label";
import { Card, CardContent } from "@multica/ui/components/ui/card";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { CEREBRO_FLAG_GROUPS, flagsForGroup, type CerebroFlagKey } from "./registry";
import {
  useFeatureFlag,
  useFeatureFlagsQuery,
  useSetFeatureFlagMutation,
  useSetWorkspaceFeatureFlagMutation,
} from "./api";
import { useFlagLocked, useWorkspaceFlagValue } from "./store";

function FlagRow({
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

  // Owner has forced this flag on for the whole team and locked it.
  const forcedForTeam = locked && workspaceValue === true;

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

  const handleTeamLockChange = (next: boolean) => {
    // On => force the flag on for everyone and lock it. Off => clear the
    // workspace override so members fall back to their personal/default value.
    workspaceMutation.mutate(
      { key: flagKey, enabled: next ? true : null, locked: next },
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
                  ? "Locked on for the whole workspace."
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
              Force on for the whole workspace (members can&apos;t turn it off)
            </Label>
            <Switch
              checked={forcedForTeam}
              disabled={workspaceMutation.isPending}
              onCheckedChange={handleTeamLockChange}
              aria-label={`Force ${label} on for the whole workspace`}
            />
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
