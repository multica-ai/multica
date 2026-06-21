"use client";

// FIR-1597. Workspace-wide default for whether a newly scheduled issue
// auto-starts its assigned agent on the start date, the due date, or not at all.
// Rendered inline under the "Start/due time of day" feature in the Cerebro
// features settings tab. Any member can read it; only admins/owners change it.
// A per-issue choice always overrides this default.
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import { Label } from "@multica/ui/components/ui/label";
import {
  NativeSelect,
  NativeSelectOption,
} from "@multica/ui/components/ui/native-select";

import type { AgentStartKind } from "../core/types";
import {
  useSetWorkspaceDateTimeSettings,
  workspaceDateTimeSettingsOptions,
} from "../core/queries";

export function AgentStartSettings() {
  const wsId = useWorkspaceId();
  const { role } = useCurrentMember(wsId);
  const isAdmin = role === "owner" || role === "admin";

  const { data, isError } = useQuery(workspaceDateTimeSettingsOptions(wsId));
  const mutation = useSetWorkspaceDateTimeSettings(wsId);

  const value: AgentStartKind = data?.default_agent_start_kind ?? "none";

  const onChange = (next: AgentStartKind) => {
    if (!isAdmin || next === value) return;
    mutation.mutate(next, {
      onError: (err) =>
        toast.error(
          err instanceof Error ? err.message : "Failed to update default",
        ),
    });
  };

  if (isError) {
    return (
      <p className="text-xs text-destructive">
        Failed to load the auto-start default.
      </p>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-4">
        <div className="space-y-0.5">
          <Label className="text-xs font-medium">Default agent auto-start</Label>
          <p className="text-xs text-muted-foreground">
            For new issues with an assigned agent: start the agent automatically
            on the start date, on the due date, or never. Any issue can override
            this. Auto-start needs a time of day set on that date.
          </p>
        </div>
        <NativeSelect
          size="sm"
          value={value}
          disabled={!isAdmin || mutation.isPending}
          onChange={(e) => onChange(e.target.value as AgentStartKind)}
          aria-label="Default agent auto-start"
        >
          <NativeSelectOption value="none">Don&apos;t start</NativeSelectOption>
          <NativeSelectOption value="start">On start date</NativeSelectOption>
          <NativeSelectOption value="due">On due date</NativeSelectOption>
        </NativeSelect>
      </div>

      {!isAdmin && (
        <p className="text-xs text-muted-foreground">
          Only workspace admins can change this default.
        </p>
      )}
    </div>
  );
}
