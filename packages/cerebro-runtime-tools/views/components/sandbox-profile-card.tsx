// FIR-2230 phase 6 — sandbox isolation profile picker on the runtime page.
//
// The OUTER WALL, in plain language: how locked-down the machine is for agents
// on this runtime. The per-tool permission table next to it is the rules INSIDE
// the wall. Picking a profile rewrites the runtime's real sandbox_policy (the
// same JSON the daemon enforces in buildSandboxConfig), so this is not a label —
// it changes what an agent can actually do on the host.
//
// "Current" reflects the server's classification of the real stored policy
// (runtime.sandbox_profile), never a hardcoded default. A hand-tuned policy that
// matches no preset shows as "Custom"; the raw policy editor elsewhere on the
// page is where such a policy is authored.

"use client";

import { useState } from "react";
import { Loader2, Lock, ShieldCheck, SlidersHorizontal } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { AgentRuntime } from "@multica/core/types";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { cn } from "@multica/ui/lib/utils";
import {
  applySandboxProfile,
  sandboxProfileCatalogOptions,
  type SandboxProfileDefinition,
} from "../sandbox-profile-data";

interface SandboxProfileCardProps {
  runtime: AgentRuntime;
  wsId: string;
  /** Edit is restricted to workspace owner/admin (server enforces this too). */
  canEdit: boolean;
}

// Missing profile (older server) means the system default, which is Developer.
const DEFAULT_PROFILE = "developer";

function profileIcon(id: string) {
  switch (id) {
    case "read_only":
      return Lock;
    case "developer":
      return ShieldCheck;
    default:
      return SlidersHorizontal;
  }
}

export function SandboxProfileCard({
  runtime,
  wsId,
  canEdit,
}: SandboxProfileCardProps) {
  const qc = useQueryClient();
  const catalogQuery = useQuery(sandboxProfileCatalogOptions(wsId));
  const current = runtime.sandbox_profile || DEFAULT_PROFILE;
  const [pendingId, setPendingId] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: (profile: string) => applySandboxProfile(runtime.id, profile),
    onMutate: (profile) => setPendingId(profile),
    onSuccess: (_data, profile) => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      const def = catalogQuery.data?.find((p) => p.id === profile);
      toast.success("Isolation profile updated", {
        description: def
          ? `${runtime.name} is now on “${def.label}”.`
          : undefined,
      });
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : "Couldn't change the isolation profile",
      );
    },
    onSettled: () => setPendingId(null),
  });

  const profiles = catalogQuery.data ?? [];
  // Always show the selectable presets; "custom" is rendered only as the current
  // state (you can't pick it — you reach it by hand-editing the raw policy).
  const selectable = profiles.filter((p) => p.selectable);
  const currentDef = profiles.find((p) => p.id === current);
  const onCustom = current === "custom";

  return (
    <div className="rounded-md border bg-card">
      <div className="border-b p-4">
        <h3 className="text-sm font-medium">Sandbox isolation</h3>
        <p className="mt-0.5 text-xs text-muted-foreground">
          The outer wall for{" "}
          <code className="font-mono text-[11px]">{runtime.name}</code> — how
          locked-down the machine is. The tools below are the rules inside this
          wall. Changing the profile changes what an agent can actually do on the
          host.
        </p>
      </div>

      <div className="p-3">
        {catalogQuery.isLoading ? (
          <div className="flex items-center gap-2 p-3 text-xs text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading profiles…
          </div>
        ) : selectable.length === 0 ? (
          <p className="p-3 text-xs text-muted-foreground">
            No isolation profiles available.
          </p>
        ) : (
          <div className="space-y-2">
            {selectable.map((profile) => (
              <ProfileOption
                key={profile.id}
                profile={profile}
                active={profile.id === current}
                disabled={!canEdit || mutation.isPending}
                pending={pendingId === profile.id}
                onSelect={() => {
                  if (profile.id !== current) mutation.mutate(profile.id);
                }}
              />
            ))}

            {onCustom && (
              <div className="flex items-start gap-2 rounded-md border border-dashed bg-muted/30 p-3">
                <SlidersHorizontal className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="text-xs">
                  <div className="flex items-center gap-2 font-medium">
                    {currentDef?.label ?? "Custom"}
                    <Badge variant="secondary" className="text-[10px]">
                      Current
                    </Badge>
                  </div>
                  <p className="mt-0.5 text-muted-foreground">
                    {currentDef?.description ??
                      "Hand-tuned rules that don't match a preset."}{" "}
                    Selecting a preset above replaces it.
                  </p>
                </div>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function ProfileOption({
  profile,
  active,
  disabled,
  pending,
  onSelect,
}: {
  profile: SandboxProfileDefinition;
  active: boolean;
  disabled: boolean;
  pending: boolean;
  onSelect: () => void;
}) {
  const Icon = profileIcon(profile.id);
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      disabled={disabled || active}
      onClick={onSelect}
      className={cn(
        "flex w-full items-start gap-3 rounded-md border p-3 text-left transition-colors",
        active
          ? "border-primary bg-primary/5"
          : "hover:bg-accent disabled:cursor-not-allowed disabled:opacity-60",
      )}
    >
      <Icon
        className={cn(
          "mt-0.5 h-4 w-4 shrink-0",
          active ? "text-primary" : "text-muted-foreground",
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-sm font-medium">
          {profile.label}
          {active && (
            <Badge variant="secondary" className="text-[10px]">
              Current
            </Badge>
          )}
          {pending && <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />}
        </div>
        <p className="mt-0.5 text-xs text-muted-foreground">{profile.description}</p>
      </div>
    </button>
  );
}
