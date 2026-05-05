"use client";

// CEREBRO-PATCH(runtime-detail): full rewrite of upstream runtime-detail.tsx.
// Wrapping was evaluated in Phase 3 (chunk 8 SA1) and rejected: cerebro and
// upstream diverge on layout, data wiring, and sibling components — no clean
// composition seam exists. Differences kept intentionally:
//   1. Sandbox-toggle section (admin-only Switch + reset-to-default + error
//      surface). Drives `useUpdateRuntimeSandbox` + `runtime.sandbox_enabled`.
//   2. PingSection (cerebro-only sibling component, not upstream).
//   3. StatusBadge + InfoField (cerebro-only exports from shared.tsx).
//      Upstream uses HealthBadge + deriveRuntimeHealth + useNowTick.
//   4. Custom metadata <pre> dump + timestamps grid at bottom.
//   5. Removed upstream presence wiring (useWorkspacePresenceMap,
//      agentListOptions, useNowTick, AgentsCard, DaemonCard).
//   6. Removed upstream breadcrumb topbar (ArrowLeft + AppLink + ChevronRight)
//      and Read-only lock indicator.
// L3 status: keep as marked patch. Resync strategy = manual review of upstream
// diffs in this file each sync cycle. See docs/upstream-sync/03-decision.md
// "Phase 3 escalation" + chunk 8 report.

import { useState } from "react";
import { Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  useDeleteRuntime,
  useUpdateRuntimeSandbox,
} from "@multica/core/runtimes/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { ActorAvatar } from "../../common/actor-avatar";
import { formatLastSeen } from "../utils";
import { StatusBadge, InfoField } from "./shared";
import { ProviderLogo } from "./provider-logo";
import { PingSection } from "./ping-section";
import { UpdateSection } from "./update-section";
import { UsageSection } from "./usage-section";

function getCliVersion(metadata: Record<string, unknown>): string | null {
  if (
    metadata &&
    typeof metadata.cli_version === "string" &&
    metadata.cli_version
  ) {
    return metadata.cli_version;
  }
  return null;
}

function getLaunchedBy(metadata: Record<string, unknown>): string | null {
  if (
    metadata &&
    typeof metadata.launched_by === "string" &&
    metadata.launched_by
  ) {
    return metadata.launched_by;
  }
  return null;
}

export function RuntimeDetail({ runtime }: { runtime: AgentRuntime }) {
  const cliVersion =
    runtime.runtime_mode === "local" ? getCliVersion(runtime.metadata) : null;
  const launchedBy =
    runtime.runtime_mode === "local" ? getLaunchedBy(runtime.metadata) : null;

  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const deleteMutation = useDeleteRuntime(wsId);
  const sandboxMutation = useUpdateRuntimeSandbox(wsId);

  const [deleteOpen, setDeleteOpen] = useState(false);

  // Resolve owner info
  const ownerMember = runtime.owner_id
    ? members.find((m) => m.user_id === runtime.owner_id) ?? null
    : null;

  // Permission check for delete
  const currentMember = user
    ? members.find((m) => m.user_id === user.id)
    : null;
  const isAdmin = currentMember
    ? currentMember.role === "owner" || currentMember.role === "admin"
    : false;
  const isRuntimeOwner = user && runtime.owner_id === user.id;
  const canDelete = isAdmin || isRuntimeOwner;

  // Sandbox toggle is admin-only on the server. We render the section to all
  // members for transparency (so a member knows their runtime IS or ISN'T
  // sandboxed) but disable the controls and surface why — silent absence
  // looked like a missing feature, and the previous toast-on-403 was easy to
  // miss on a slow network.
  const canEditSandbox = isAdmin;
  const sandboxOverride = runtime.sandbox_enabled;
  // For the binary Switch we treat null (no override) as "on" — that matches
  // the daemon-wide darwin default and keeps the control truthful for the
  // overwhelmingly common case. The "Reset to default" link below shows up
  // whenever an explicit override is in place, so the inherit state is
  // recoverable without a tri-state control.
  const sandboxChecked = sandboxOverride === null ? true : sandboxOverride;
  const [sandboxError, setSandboxError] = useState<string | null>(null);

  const handleSandboxMutate = (next: boolean | null) => {
    setSandboxError(null);
    sandboxMutation.mutate(
      { runtimeId: runtime.id, sandboxEnabled: next },
      {
        onError: (e) => {
          // Server returns a JSON {error: "..."} body. The api client surfaces
          // it as Error.message — show it verbatim so the user sees the actual
          // reason (permission, validation, server-side state) instead of a
          // generic "Failed".
          const msg = e instanceof Error ? e.message : "Unknown error";
          setSandboxError(msg);
        },
      },
    );
  };

  const handleDelete = () => {
    deleteMutation.mutate(runtime.id, {
      onSuccess: () => {
        toast.success("Runtime deleted");
        setDeleteOpen(false);
      },
      onError: (e) => {
        toast.error(e instanceof Error ? e.message : "Failed to delete runtime");
      },
    });
  };

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
        <div className="flex min-w-0 items-center gap-2">
          <div className="flex h-7 w-7 shrink-0 items-center justify-center">
            <ProviderLogo provider={runtime.provider} className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <h2 className="text-sm font-semibold truncate">{runtime.name}</h2>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <StatusBadge status={runtime.status} />
          {canDelete && (
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-muted-foreground hover:text-destructive"
              onClick={() => setDeleteOpen(true)}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-4 sm:p-6 space-y-6">
        {/* Info grid */}
        <div className="grid grid-cols-2 gap-4">
          <InfoField label="Runtime Mode" value={runtime.runtime_mode} />
          <InfoField label="Provider" value={runtime.provider} />
          <InfoField label="Status" value={runtime.status} />
          <InfoField
            label="Last Seen"
            value={formatLastSeen(runtime.last_seen_at)}
          />
          {ownerMember && (
            <div>
              <div className="text-xs text-muted-foreground mb-1">Owner</div>
              <div className="flex items-center gap-2">
                <ActorAvatar
                  actorType="member"
                  actorId={ownerMember.user_id}
                  size={20}
                />
                <span className="text-sm">{ownerMember.name}</span>
              </div>
            </div>
          )}
          {runtime.device_info && (
            <InfoField label="Device" value={runtime.device_info} />
          )}
          {runtime.daemon_id && (
            <InfoField label="Daemon ID" value={runtime.daemon_id} mono />
          )}
        </div>

        {/* CLI Version & Update */}
        {runtime.runtime_mode === "local" && (
          <div>
            <h3 className="text-xs font-medium text-muted-foreground mb-3">
              CLI Version
            </h3>
            <UpdateSection
              runtimeId={runtime.id}
              currentVersion={cliVersion}
              isOnline={runtime.status === "online"}
              launchedBy={launchedBy}
            />
          </div>
        )}

        {/* Connection Test */}
        <div>
          <h3 className="text-xs font-medium text-muted-foreground mb-3">
            Connection Test
          </h3>
          <PingSection runtimeId={runtime.id} />
        </div>

        {/* Sandbox toggle — local runtimes only. The macOS seatbelt sandbox
            is a no-op elsewhere, so showing the control on a Linux cloud
            runtime would just confuse operators. The control is rendered to
            every member for transparency about the current state, but the
            toggle is interactive only for owner/admin (matches the server
            permission gate). */}
        {runtime.runtime_mode === "local" && (
          <div>
            <h3 className="text-xs font-medium text-muted-foreground mb-3">
              Sandbox
            </h3>
            <div className="rounded-lg border p-4">
              <div className="flex items-start justify-between gap-4">
                <div className="space-y-1">
                  <div className="text-sm font-medium">
                    Run agent CLIs in macOS sandbox
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {sandboxOverride === null
                      ? "Inheriting the daemon's MULTICA_ENABLE_SANDBOX setting (default on macOS)."
                      : sandboxOverride
                        ? "Sandbox is forced on for this runtime."
                        : "Sandbox is disabled for this runtime — agents run with full host access."}
                  </p>
                </div>
                <Switch
                  checked={sandboxChecked}
                  onCheckedChange={(next) => handleSandboxMutate(next)}
                  disabled={!canEditSandbox || sandboxMutation.isPending}
                  aria-label="Sandbox enabled"
                />
              </div>
              {!canEditSandbox && (
                <p className="mt-3 text-xs text-muted-foreground italic">
                  Only workspace owners and admins can change this setting.
                </p>
              )}
              {canEditSandbox && sandboxOverride !== null && (
                <button
                  type="button"
                  className="mt-3 text-xs text-muted-foreground underline-offset-2 hover:underline disabled:opacity-50"
                  onClick={() => handleSandboxMutate(null)}
                  disabled={sandboxMutation.isPending}
                >
                  Reset to default
                </button>
              )}
              {sandboxError && (
                <div
                  role="alert"
                  className="mt-3 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-xs"
                >
                  <div className="font-medium text-destructive">
                    Couldn&apos;t update sandbox setting
                  </div>
                  <p className="mt-1 text-destructive/90 break-words">
                    {sandboxError}
                  </p>
                  <button
                    type="button"
                    className="mt-2 text-destructive/80 underline-offset-2 hover:underline"
                    onClick={() => setSandboxError(null)}
                  >
                    Dismiss
                  </button>
                </div>
              )}
            </div>
          </div>
        )}

        {/* Usage */}
        <div>
          <h3 className="text-xs font-medium text-muted-foreground mb-3">
            Token Usage
          </h3>
          <UsageSection runtimeId={runtime.id} />
        </div>

        {/* Metadata */}
        {runtime.metadata && Object.keys(runtime.metadata).length > 0 && (
          <div>
            <h3 className="text-xs font-medium text-muted-foreground mb-2">
              Metadata
            </h3>
            <div className="rounded-lg border bg-muted/30 p-3">
              <pre className="text-xs font-mono whitespace-pre-wrap break-all">
                {JSON.stringify(runtime.metadata, null, 2)}
              </pre>
            </div>
          </div>
        )}

        {/* Timestamps */}
        <div className="grid grid-cols-2 gap-4 border-t pt-4">
          <InfoField
            label="Created"
            value={new Date(runtime.created_at).toLocaleString()}
          />
          <InfoField
            label="Updated"
            value={new Date(runtime.updated_at).toLocaleString()}
          />
        </div>
      </div>

      {/* Delete confirmation */}
      <AlertDialog open={deleteOpen} onOpenChange={(v) => { if (!v) setDeleteOpen(false); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Runtime</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete &ldquo;{runtime.name}&rdquo;? This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? "Deleting..." : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
