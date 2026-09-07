import type { AgentRuntime } from "@multica/core/types";

export type ManagedRuntimeSetupPhase = "installing" | "ready" | "failed";

export interface ManagedRuntimeSetupStatus {
  provider: string;
  phase: ManagedRuntimeSetupPhase;
  startedAt: string;
  version?: string;
  source?: "user" | "managed";
  /**
   * Why the install failed, in the installer's own words. Set only on the
   * "failed" phase; without it the UI can say nothing beyond "failed".
   * Mirrors the same field on the desktop DaemonStatus payload.
   */
  error?: string;
}

const PENDING_MANAGED_RUNTIME_ID_PREFIX = "pending-managed-runtime:";

interface PendingManagedRuntimeMetadata extends Record<string, unknown> {
  pending_managed_runtime: true;
  managed_runtime_phase: ManagedRuntimeSetupPhase;
  managed_runtime_started_at: string;
  managed_runtime_version?: string;
  managed_runtime_source?: "user" | "managed";
}

function providerLabel(provider: string): string {
  const normalized = provider.trim().toLowerCase();
  if (normalized === "pi") return "Pi";
  return normalized
    ? `${normalized.charAt(0).toUpperCase()}${normalized.slice(1)}`
    : "Runtime";
}

export function pendingManagedRuntimeId(provider: string): string {
  return `${PENDING_MANAGED_RUNTIME_ID_PREFIX}${provider.trim().toLowerCase()}`;
}

export function isPendingManagedRuntime(runtime: AgentRuntime): boolean {
  return runtime.metadata?.pending_managed_runtime === true;
}

export function managedRuntimeSetupPhase(
  runtime: AgentRuntime,
): ManagedRuntimeSetupPhase | null {
  if (!isPendingManagedRuntime(runtime)) return null;
  const phase = runtime.metadata?.managed_runtime_phase;
  return phase === "installing" || phase === "ready" || phase === "failed"
    ? phase
    : null;
}

export function managedRuntimeSetupVersion(
  runtime: AgentRuntime,
): string | null {
  if (!isPendingManagedRuntime(runtime)) return null;
  const version = runtime.metadata?.managed_runtime_version;
  return typeof version === "string" && version.trim() ? version : null;
}

export function pendingManagedRuntimeFromSetup({
  setup,
  workspaceId,
  localDaemonId,
  localMachineName,
  fallbackMachineName,
}: {
  setup: ManagedRuntimeSetupStatus;
  workspaceId: string;
  localDaemonId?: string | null;
  localMachineName?: string | null;
  fallbackMachineName?: string | null;
}): AgentRuntime {
  const provider = setup.provider.trim().toLowerCase();
  const label = providerLabel(provider);
  const machineName =
    localMachineName?.trim() || fallbackMachineName?.trim() || "Runtime";
  const metadata: PendingManagedRuntimeMetadata = {
    pending_managed_runtime: true,
    managed_runtime_phase: setup.phase,
    managed_runtime_started_at: setup.startedAt,
    ...(setup.version ? { managed_runtime_version: setup.version } : {}),
    ...(setup.source ? { managed_runtime_source: setup.source } : {}),
  };

  return {
    id: pendingManagedRuntimeId(provider),
    workspace_id: workspaceId,
    daemon_id: localDaemonId ?? null,
    name: `${label} (${machineName})`,
    runtime_mode: "local",
    provider,
    launch_header: provider,
    status: "offline",
    device_info: machineName,
    metadata,
    owner_id: null,
    visibility: "private",
    profile_id: null,
    last_seen_at: null,
    created_at: setup.startedAt,
    updated_at: setup.startedAt,
  };
}

export function runtimesWithManagedRuntimeSetup({
  runtimes,
  setup,
  workspaceId,
  localDaemonId,
  localMachineName,
  fallbackMachineName,
}: {
  runtimes: AgentRuntime[];
  setup?: ManagedRuntimeSetupStatus | null;
  workspaceId: string;
  localDaemonId?: string | null;
  localMachineName?: string | null;
  fallbackMachineName?: string | null;
}): AgentRuntime[] {
  if (!setup) return runtimes;
  const provider = setup.provider.trim().toLowerCase();
  const hasRegisteredBuiltIn = runtimes.some(
    (runtime) =>
      !runtime.profile_id && runtime.provider.trim().toLowerCase() === provider,
  );
  if (hasRegisteredBuiltIn) return runtimes;

  return [
    ...runtimes,
    pendingManagedRuntimeFromSetup({
      setup,
      workspaceId,
      localDaemonId,
      localMachineName,
      fallbackMachineName,
    }),
  ];
}
