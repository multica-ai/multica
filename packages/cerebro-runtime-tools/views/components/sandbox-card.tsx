// FIR-2284 Bite 1 — the consolidated Sandbox surface for a runtime.
//
// Before this card the sandbox lived in THREE separate boxes on the runtime
// page and confused operators (see the issue thread): an on/off switch, a
// Developer/Read-only profile picker, and a raw "Runtime sandbox policy" editor
// full of technical free-text fields. They are not three systems — they are one
// setting shown three ways. This card folds them into a single progressive
// surface:
//
//   1. One on/off switch at the top. Off → nothing else shows (no technical
//      noise); the machine simply runs with full host access.
//   2. When on → pick a preset (Developer / Read-only / Custom). Selecting a
//      preset rewrites the runtime's real sandbox_policy server-side.
//   3. Advanced (collapsed by default) → the exact policy fields, with the
//      plain-language choices surfaced as selects. Editing them reclassifies
//      the runtime as "Custom".
//
// Same underlying state as before — this is a UI consolidation, not a new
// engine. The on/off and policy mutations are passed in by the runtime page so
// optimistic cache updates stay owned in one place; the preset PATCH is local
// to this card (same call the old profile picker used).

"use client";

import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ChevronDown,
  Loader2,
  Lock,
  Network,
  Shield,
  ShieldCheck,
  SlidersHorizontal,
  Terminal,
} from "lucide-react";
import { toast } from "sonner";
import type { AgentRuntime } from "@multica/core/types";
import type {
  useUpdateRuntimeSandbox,
  useUpdateRuntimeSandboxPolicy,
} from "@multica/core/runtimes/mutations";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import {
  applySandboxProfile,
  sandboxProfileCatalogOptions,
} from "../sandbox-profile-data";

// Missing profile (older server) means the system default, which is Developer.
const DEFAULT_PROFILE = "developer";

interface SandboxCardProps {
  runtime: AgentRuntime;
  wsId: string;
  /** Edit is restricted to workspace owner/admin (server enforces this too). */
  canEdit: boolean;
  sandboxMutation: ReturnType<typeof useUpdateRuntimeSandbox>;
  sandboxPolicyMutation: ReturnType<typeof useUpdateRuntimeSandboxPolicy>;
}

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

export function SandboxCard({
  runtime,
  wsId,
  canEdit,
  sandboxMutation,
  sandboxPolicyMutation,
}: SandboxCardProps) {
  const qc = useQueryClient();

  // --- on/off ---------------------------------------------------------------
  // null override is treated as "on" for the binary switch — matches the
  // daemon-wide darwin default. "Reset to default" recovers the inherit state.
  const sandboxOverride = runtime.sandbox_enabled;
  const sandboxOn = sandboxOverride === null ? true : sandboxOverride;
  const [sandboxError, setSandboxError] = useState<string | null>(null);

  const handleSandboxMutate = (next: boolean | null) => {
    setSandboxError(null);
    sandboxMutation.mutate(
      { runtimeId: runtime.id, sandboxEnabled: next },
      {
        onError: (e) =>
          setSandboxError(e instanceof Error ? e.message : "Unknown error"),
      },
    );
  };

  // --- preset ---------------------------------------------------------------
  const catalogQuery = useQuery(sandboxProfileCatalogOptions(wsId));
  const profiles = catalogQuery.data ?? [];
  const selectable = profiles.filter((p) => p.selectable);
  const currentProfile = runtime.sandbox_profile || DEFAULT_PROFILE;
  const currentDef = profiles.find((p) => p.id === currentProfile);
  const onCustom = currentProfile === "custom";
  const [pendingProfile, setPendingProfile] = useState<string | null>(null);

  const profileMutation = useMutation({
    mutationFn: (profile: string) => applySandboxProfile(runtime.id, profile),
    onMutate: (profile) => setPendingProfile(profile),
    onSuccess: (_data, profile) => {
      qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
      const def = profiles.find((p) => p.id === profile);
      toast.success("Sandbox preset updated", {
        description: def ? `${runtime.name} is now on “${def.label}”.` : undefined,
      });
    },
    onError: (err) =>
      toast.error(
        err instanceof Error ? err.message : "Couldn't change the sandbox preset",
      ),
    onSettled: () => setPendingProfile(null),
  });

  // --- advanced policy ------------------------------------------------------
  const policy = runtime.sandbox_policy ?? {};
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [networkMode, setNetworkMode] = useState<"default" | "custom">(
    policy.network_mode ?? "default",
  );
  const [allowedHosts, setAllowedHosts] = useState(
    (policy.allowed_hosts ?? []).join("\n"),
  );
  const [extraWritablePaths, setExtraWritablePaths] = useState(
    (policy.extra_writable_paths ?? []).join("\n"),
  );
  const [deniedPaths, setDeniedPaths] = useState(
    (policy.denied_paths ?? []).join("\n"),
  );
  const [denyShell, setDenyShell] = useState(Boolean(policy.deny_shell));
  const [policyError, setPolicyError] = useState<string | null>(null);

  useEffect(() => {
    const next = runtime.sandbox_policy ?? {};
    setNetworkMode(next.network_mode ?? "default");
    setAllowedHosts((next.allowed_hosts ?? []).join("\n"));
    setExtraWritablePaths((next.extra_writable_paths ?? []).join("\n"));
    setDeniedPaths((next.denied_paths ?? []).join("\n"));
    setDenyShell(Boolean(next.deny_shell));
  }, [runtime.sandbox_policy]);

  const lines = (value: string) =>
    value
      .split(/\r?\n|,/)
      .map((item) => item.trim())
      .filter(Boolean);

  const handlePolicySave = () => {
    setPolicyError(null);
    sandboxPolicyMutation.mutate(
      {
        runtimeId: runtime.id,
        sandboxPolicy: {
          network_mode: networkMode,
          allowed_hosts: lines(allowedHosts),
          extra_writable_paths: lines(extraWritablePaths),
          denied_paths: lines(deniedPaths),
          deny_shell: denyShell,
        },
      },
      {
        onError: (e) =>
          setPolicyError(e instanceof Error ? e.message : "Unknown error"),
      },
    );
  };

  const busy = sandboxMutation.isPending || profileMutation.isPending;

  return (
    <div className="rounded-lg border">
      <div className="flex items-start justify-between gap-3 border-b px-4 py-3">
        <div className="space-y-0.5">
          <div className="flex items-center gap-2 text-sm font-medium">
            <Shield className="h-4 w-4 text-muted-foreground" />
            Sandbox
          </div>
          <p className="text-xs text-muted-foreground">
            The outer wall around{" "}
            <code className="font-mono text-[11px]">{runtime.name}</code> — how
            locked-down the machine is. The tools list is the rules inside this
            wall.
          </p>
        </div>
        <Switch
          checked={sandboxOn}
          onCheckedChange={(next) => handleSandboxMutate(next)}
          disabled={!canEdit || sandboxMutation.isPending}
          aria-label="Sandbox enabled"
        />
      </div>

      <div className="space-y-3 p-4">
        <p className="text-xs text-muted-foreground">
          {sandboxOverride === null
            ? "Inheriting the daemon default (sandbox on for macOS)."
            : sandboxOn
              ? "Sandbox is on — agents are confined to the rules below."
              : "Sandbox is off — agents run with full host access."}
        </p>

        {!canEdit && (
          <p className="text-xs italic text-muted-foreground">
            Only workspace owners and admins can change this.
          </p>
        )}

        {canEdit && sandboxOverride !== null && (
          <button
            type="button"
            className="text-xs text-muted-foreground underline-offset-2 hover:underline disabled:opacity-50"
            onClick={() => handleSandboxMutate(null)}
            disabled={sandboxMutation.isPending}
          >
            Reset to default
          </button>
        )}

        {sandboxError && (
          <div
            role="alert"
            className="rounded-md border border-destructive/50 bg-destructive/10 p-2 text-xs"
          >
            <div className="font-medium text-destructive">
              Couldn&apos;t update sandbox setting
            </div>
            <p className="mt-1 break-words text-destructive/90">{sandboxError}</p>
            <button
              type="button"
              className="mt-1.5 text-destructive/80 underline-offset-2 hover:underline"
              onClick={() => setSandboxError(null)}
            >
              Dismiss
            </button>
          </div>
        )}

        {/* Everything below only matters once the wall is up. */}
        {sandboxOn && (
          <div className="space-y-3 border-t pt-3">
            {/* Preset picker */}
            <div className="space-y-1.5">
              <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                Preset
              </div>
              {catalogQuery.isLoading ? (
                <div className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading
                  presets…
                </div>
              ) : selectable.length === 0 ? (
                <p className="text-xs text-muted-foreground">
                  No sandbox presets available.
                </p>
              ) : (
                <div className="space-y-2">
                  {selectable.map((profile) => {
                    const Icon = profileIcon(profile.id);
                    const active = profile.id === currentProfile;
                    return (
                      <button
                        key={profile.id}
                        type="button"
                        role="radio"
                        aria-checked={active}
                        disabled={!canEdit || busy || active}
                        onClick={() => {
                          if (profile.id !== currentProfile)
                            profileMutation.mutate(profile.id);
                        }}
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
                            {pendingProfile === profile.id && (
                              <Loader2 className="h-3 w-3 animate-spin text-muted-foreground" />
                            )}
                          </div>
                          <p className="mt-0.5 text-xs text-muted-foreground">
                            {profile.description}
                          </p>
                        </div>
                      </button>
                    );
                  })}

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
                          Pick a preset above to replace it.
                        </p>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>

            {/* Advanced policy — collapsed by default so a normal operator
                never has to look at hostnames and paths. */}
            <div className="rounded-md border">
              <button
                type="button"
                onClick={() => setAdvancedOpen((v) => !v)}
                className="flex w-full items-center justify-between gap-2 px-3 py-2 text-xs font-medium"
                aria-expanded={advancedOpen}
              >
                <span className="flex items-center gap-1.5">
                  <SlidersHorizontal className="h-3.5 w-3.5 text-muted-foreground" />
                  Advanced settings
                </span>
                <ChevronDown
                  className={cn(
                    "h-4 w-4 text-muted-foreground transition-transform",
                    advancedOpen && "rotate-180",
                  )}
                />
              </button>

              {advancedOpen && (
                <div className="space-y-3 border-t p-3">
                  <div className="grid gap-3 md:grid-cols-2">
                    <label className="space-y-1.5">
                      <span className="flex items-center gap-1.5 text-xs font-medium">
                        <Network className="h-3.5 w-3.5 text-muted-foreground" />
                        Network access
                      </span>
                      <select
                        className="h-8 w-full rounded-md border bg-background px-2 text-xs disabled:opacity-60"
                        value={networkMode}
                        onChange={(e) =>
                          setNetworkMode(e.target.value as "default" | "custom")
                        }
                        disabled={!canEdit || sandboxPolicyMutation.isPending}
                      >
                        <option value="default">
                          Full access (provider defaults)
                        </option>
                        <option value="custom">
                          Only the hosts listed below
                        </option>
                      </select>
                    </label>
                    <label className="space-y-1.5">
                      <span className="flex items-center gap-1.5 text-xs font-medium">
                        <Terminal className="h-3.5 w-3.5 text-muted-foreground" />
                        Terminal access
                      </span>
                      <select
                        className="h-8 w-full rounded-md border bg-background px-2 text-xs disabled:opacity-60"
                        value={denyShell ? "blocked" : "allowed"}
                        onChange={(e) =>
                          setDenyShell(e.target.value === "blocked")
                        }
                        disabled={!canEdit || sandboxPolicyMutation.isPending}
                      >
                        <option value="allowed">Allow shell commands</option>
                        <option value="blocked">Block shell escapes</option>
                      </select>
                    </label>
                  </div>

                  {networkMode === "custom" && (
                    <label className="space-y-1.5">
                      <span className="text-xs font-medium">Allowed hosts</span>
                      <textarea
                        className="min-h-16 w-full resize-y rounded-md border bg-background p-2 font-mono text-xs disabled:opacity-60"
                        value={allowedHosts}
                        onChange={(e) => setAllowedHosts(e.target.value)}
                        disabled={!canEdit || sandboxPolicyMutation.isPending}
                        placeholder="proxy.internal:8443"
                      />
                    </label>
                  )}

                  <div className="grid gap-3 md:grid-cols-2">
                    <label className="space-y-1.5">
                      <span className="text-xs font-medium">Writable paths</span>
                      <textarea
                        className="min-h-16 w-full resize-y rounded-md border bg-background p-2 font-mono text-xs disabled:opacity-60"
                        value={extraWritablePaths}
                        onChange={(e) => setExtraWritablePaths(e.target.value)}
                        disabled={!canEdit || sandboxPolicyMutation.isPending}
                        placeholder="/Users/sara/.cache/multica"
                      />
                    </label>
                    <label className="space-y-1.5">
                      <span className="text-xs font-medium">Denied paths</span>
                      <textarea
                        className="min-h-16 w-full resize-y rounded-md border bg-background p-2 font-mono text-xs disabled:opacity-60"
                        value={deniedPaths}
                        onChange={(e) => setDeniedPaths(e.target.value)}
                        disabled={!canEdit || sandboxPolicyMutation.isPending}
                        placeholder="/Users/sara/Documents/private"
                      />
                    </label>
                  </div>

                  {policyError && (
                    <div
                      role="alert"
                      className="rounded-md border border-destructive/50 bg-destructive/10 p-2 text-xs text-destructive"
                    >
                      {policyError}
                    </div>
                  )}

                  <div className="flex justify-end">
                    <Button
                      type="button"
                      size="sm"
                      className="h-8"
                      onClick={handlePolicySave}
                      disabled={!canEdit || sandboxPolicyMutation.isPending}
                    >
                      {sandboxPolicyMutation.isPending && (
                        <Loader2 className="h-3.5 w-3.5 animate-spin" />
                      )}
                      Save advanced settings
                    </Button>
                  </div>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
