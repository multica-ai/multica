import { deriveRuntimeHealth, type RuntimeHealth } from "@multica/core/runtimes";
import type { AgentRuntime } from "@multica/core/types";
import { formatDeviceInfo } from "../utils";

export type RuntimeMachineSection = "local" | "remote" | "cloud";
export type RuntimeMachineFilter = "all" | "online" | "issues";

export interface RuntimeWorkloadSummary {
  runningCount: number;
  queuedCount: number;
}

export interface RuntimeMachine {
  id: string;
  daemonId: string | null;
  title: string;
  subtitle: string | null;
  deviceInfo: string | null;
  cliVersion: string | null;
  mode: AgentRuntime["runtime_mode"];
  section: RuntimeMachineSection;
  isCurrent: boolean;
  health: RuntimeHealth;
  runtimes: AgentRuntime[];
  onlineCount: number;
  issueCount: number;
  runningCount: number;
  queuedCount: number;
  providerNames: string[];
  lastSeenAt: string | null;
}

interface RuntimeMachineOptions {
  now: number;
  localDaemonId?: string | null;
  localMachineName?: string | null;
  /**
   * The viewing user's id. Used to scope the device-name consolidation
   * below: the runtime list is workspace-wide (every member's runtimes),
   * so matching purely on a host name could promote another member's
   * identically-named machine to "this machine". Only a local runtime
   * OWNED by the current user may be consolidated by device name.
   */
  currentUserId?: string | null;
  workloadByRuntimeId?: Map<string, RuntimeWorkloadSummary>;
  /**
   * When true, guarantee that the result contains a machine flagged
   * `isCurrent`. If no server-side runtime matches the local daemon
   * (e.g. the daemon is stopped, was never started, or its runtime was
   * already GC'd), a placeholder local machine is synthesized so the
   * caller can still attach controls to it (Start button, etc.).
   * Desktop sets this; web omits it.
   */
  ensureLocalMachine?: boolean;
}

interface RuntimeMachineDraft {
  id: string;
  daemonId: string | null;
  mode: AgentRuntime["runtime_mode"];
  runtimes: AgentRuntime[];
}

const HEALTH_SEVERITY: Record<RuntimeHealth, number> = {
  online: 0,
  recently_lost: 1,
  offline: 2,
  about_to_gc: 3,
};

export function splitRuntimeName(name: string): {
  base: string;
  hostname: string | null;
} {
  const m = name.match(/^(.+?)\s+\(([^)]+)\)$/);
  if (!m || !m[1] || !m[2]) return { base: name, hostname: null };
  return { base: m[1], hostname: m[2] };
}

// The label for a runtime rendered under (or next to) its machine's name.
// A machine-level rename stamps the same custom_name on every runtime of
// the daemon (MUL-4217), so repeating it per runtime is noise — fall back
// to the provider base (e.g. "Claude"). A one-off per-runtime rename that
// differs from the machine name stays visible verbatim.
export function runtimeRowLabel(
  runtime: AgentRuntime,
  machineTitle: string,
): string {
  const custom = runtime.custom_name?.trim();
  if (custom && custom !== machineTitle) return custom;
  return splitRuntimeName(runtime.name).base;
}

export function buildRuntimeMachines(
  runtimes: AgentRuntime[],
  options: RuntimeMachineOptions,
): RuntimeMachine[] {
  const drafts = new Map<string, RuntimeMachineDraft>();

  for (const runtime of runtimes) {
    const id = runtimeMachineId(runtime);
    const draft =
      drafts.get(id) ??
      ({
        id,
        daemonId: runtime.daemon_id,
        mode: runtime.runtime_mode,
        runtimes: [],
      } satisfies RuntimeMachineDraft);
    draft.runtimes.push(runtime);
    drafts.set(id, draft);
  }

  const machines = Array.from(drafts.values()).map((draft) =>
    finalizeRuntimeMachine(draft, options),
  );

  if (options.ensureLocalMachine && !machines.some((m) => m.isCurrent)) {
    machines.push(placeholderLocalMachine(options));
  }

  return machines.sort(compareRuntimeMachines);
}

function placeholderLocalMachine(
  options: RuntimeMachineOptions,
): RuntimeMachine {
  const daemonId = options.localDaemonId ?? null;
  return {
    id: daemonId ? `local:${daemonId}` : "local:placeholder",
    daemonId,
    title: options.localMachineName ?? "This machine",
    subtitle: null,
    deviceInfo: null,
    cliVersion: null,
    mode: "local",
    section: "local",
    isCurrent: true,
    health: "offline",
    runtimes: [],
    onlineCount: 0,
    issueCount: 0,
    runningCount: 0,
    queuedCount: 0,
    providerNames: [],
    lastSeenAt: null,
  };
}

export function filterRuntimeMachines(
  machines: RuntimeMachine[],
  query: string,
  filter: RuntimeMachineFilter,
): RuntimeMachine[] {
  const q = query.trim().toLowerCase();
  return machines.filter((machine) => {
    if (filter === "online" && machine.onlineCount === 0) return false;
    if (filter === "issues" && machine.issueCount === 0) return false;
    if (!q) return true;

    const haystack = [
      machine.title,
      machine.subtitle,
      machine.deviceInfo,
      machine.daemonId,
      machine.providerNames.join(" "),
      machine.runtimes.map((runtime) => runtime.name).join(" "),
      machine.runtimes.map((runtime) => runtime.custom_name ?? "").join(" "),
    ]
      .filter(Boolean)
      .join(" ")
      .toLowerCase();

    return haystack.includes(q);
  });
}

export function runtimeMachineCounts(machines: RuntimeMachine[]): {
  all: number;
  online: number;
  issues: number;
} {
  return {
    all: machines.length,
    online: machines.filter((machine) => machine.onlineCount > 0).length,
    issues: machines.filter((machine) => machine.issueCount > 0).length,
  };
}

function finalizeRuntimeMachine(
  draft: RuntimeMachineDraft,
  options: RuntimeMachineOptions,
): RuntimeMachine {
  const runtimes = draft.runtimes.toSorted((a, b) =>
    a.provider.localeCompare(b.provider),
  );
  const first = runtimes[0];
  const providerNames = Array.from(new Set(runtimes.map((r) => r.provider))).sort();
  // Device-name consolidation is only safe for the current user's own
  // local runtimes — the list spans the whole workspace, so a host-name
  // match alone could claim another member's identically-named machine.
  const ownsLocalRuntime =
    !!options.currentUserId &&
    runtimes.some((r) => r.owner_id === options.currentUserId);
  const matchesLocalName = (value: string | null | undefined): boolean =>
    !!value && value.toLowerCase() === options.localMachineName?.toLowerCase();
  const isCurrent =
    (!!options.localDaemonId && draft.daemonId === options.localDaemonId) ||
    (draft.mode === "local" &&
      !!options.localMachineName &&
      ownsLocalRuntime &&
      (matchesLocalName(draft.daemonId) ||
        runtimes.some((r) => matchesLocalName(runtimeDeviceName(r)))));
  const title = machineTitle(runtimes, {
    isCurrent,
    localMachineName: options.localMachineName,
  });
  const deviceInfo = first ? formatDeviceInfo(first.device_info ?? null) : null;
  const subtitle = machineSubtitle({
    title,
    deviceInfo,
    daemonId: draft.daemonId,
    mode: draft.mode,
  });
  const healthByRuntime = runtimes.map((runtime) =>
    deriveRuntimeHealth(runtime, options.now),
  );
  const onlineCount = healthByRuntime.filter((h) => h === "online").length;
  const issueCount = runtimes.length - onlineCount;
  const health =
    onlineCount > 0
      ? "online"
      : healthByRuntime.reduce<RuntimeHealth>(
          (worst, current) =>
            HEALTH_SEVERITY[current] > HEALTH_SEVERITY[worst] ? current : worst,
          "recently_lost",
        );
  const workload = runtimes.reduce(
    (sum, runtime) => {
      const entry = options.workloadByRuntimeId?.get(runtime.id);
      return {
        runningCount: sum.runningCount + (entry?.runningCount ?? 0),
        queuedCount: sum.queuedCount + (entry?.queuedCount ?? 0),
      };
    },
    { runningCount: 0, queuedCount: 0 },
  );

  return {
    id: draft.id,
    daemonId: draft.daemonId,
    title,
    subtitle,
    deviceInfo,
    cliVersion: commonCliVersion(runtimes),
    mode: draft.mode,
    section: isCurrent ? "local" : draft.mode === "cloud" ? "cloud" : "remote",
    isCurrent,
    health,
    runtimes,
    onlineCount,
    issueCount,
    runningCount: workload.runningCount,
    queuedCount: workload.queuedCount,
    providerNames,
    lastSeenAt: latestLastSeenAt(runtimes),
  };
}

function runtimeMachineId(runtime: AgentRuntime): string {
  if (runtime.daemon_id) return `${runtime.runtime_mode}:${runtime.daemon_id}`;
  const deviceName = runtimeDeviceName(runtime);
  if (deviceName) return `${runtime.runtime_mode}:device:${deviceName}`;
  return `${runtime.runtime_mode}:runtime:${runtime.id}`;
}

function runtimeDeviceName(runtime: AgentRuntime): string | null {
  const host = splitRuntimeName(runtime.name).hostname;
  if (host) return host;

  const raw = runtime.device_info?.trim();
  if (!raw) return null;
  return raw.split(" · ")[0]?.trim() || null;
}

// The custom name shared by every runtime on a machine (MUL-4217). A
// machine-level rename writes the same custom_name to all runtimes on the
// daemon, so a name shared by all of them is the machine's label. A one-off
// per-runtime rename (not shared) is deliberately ignored here so it can't
// masquerade as the whole machine's name.
export function sharedCustomName(runtimes: AgentRuntime[]): string | null {
  if (runtimes.length === 0) return null;
  const names = runtimes.map((r) => r.custom_name?.trim() ?? "");
  const first = names[0];
  if (!first) return null;
  return names.every((n) => n === first) ? first : null;
}

function machineTitle(
  runtimes: AgentRuntime[],
  options: { isCurrent: boolean; localMachineName?: string | null },
): string {
  // An explicit user-set machine name wins over everything, including the
  // OS-reported local machine name.
  const shared = sharedCustomName(runtimes);
  if (shared) return shared;

  if (options.isCurrent && options.localMachineName) {
    return options.localMachineName;
  }

  const first = runtimes[0];
  if (!first) return "Unknown machine";

  const deviceName = runtimeDeviceName(first);
  if (deviceName) return deviceName;

  if (first.runtime_mode === "cloud") {
    return `${capitalize(first.provider)} cloud`;
  }
  return first.daemon_id ? shortDaemonId(first.daemon_id) : "Unknown machine";
}

function machineSubtitle({
  title,
  deviceInfo,
  daemonId,
  mode,
}: {
  title: string;
  deviceInfo: string | null;
  daemonId: string | null;
  mode: AgentRuntime["runtime_mode"];
}): string | null {
  const compact = compactDeviceInfo(deviceInfo, title);
  if (compact) return compact;
  if (daemonId) return `daemon ${shortDaemonId(daemonId)}`;
  return mode === "cloud" ? "Cloud worker" : null;
}

function compactDeviceInfo(
  deviceInfo: string | null,
  title: string,
): string | null {
  if (!deviceInfo) return null;
  const parts = deviceInfo
    .split(" · ")
    .map((part) => part.trim())
    .filter(Boolean)
    .filter((part) => part !== title)
    .filter((part) => !isAgentVersionLike(part));
  const primary = parts[0];
  if (!primary) return null;

  // Reshape OS+arch produced by formatDeviceInfo (e.g. "macOS (x86_64)")
  // into the more scannable "x86_64 macOS". Version strings — the only
  // other shape that historically carried parens — are filtered out
  // above so they can't pollute the per-machine subtitle.
  const osArch = primary.match(/^(.+?)\s+\(([^)]+)\)$/);
  if (osArch?.[1] && osArch[2]) {
    return `${osArch[2]} ${osArch[1]}`;
  }
  return primary;
}

// True for parts that carry an agent CLI version, not machine info —
// e.g. "2.1.5 (Claude Code)", "codex-cli 0.118.0", "1.0.20", "claude 1.0.0".
// Those describe a runtime, not the host, so they should never become a
// machine's subtitle (otherwise every claude-equipped daemon's row reads
// "Claude Code …", drowning out actual per-machine differences).
function isAgentVersionLike(part: string): boolean {
  return /(?:^|\s)v?\d+\.\d+\.\d+/.test(part);
}

function latestLastSeenAt(runtimes: AgentRuntime[]): string | null {
  let latest: string | null = null;
  for (const runtime of runtimes) {
    if (!runtime.last_seen_at) continue;
    if (!latest || new Date(runtime.last_seen_at) > new Date(latest)) {
      latest = runtime.last_seen_at;
    }
  }
  return latest;
}

function commonCliVersion(runtimes: AgentRuntime[]): string | null {
  const versions = new Set<string>();
  for (const runtime of runtimes) {
    const version = runtime.metadata?.cli_version;
    if (typeof version === "string" && version.trim()) {
      versions.add(version.trim());
    }
  }
  return versions.size === 1 ? Array.from(versions)[0] ?? null : null;
}

function shortDaemonId(daemonId: string): string {
  return daemonId.length > 12 ? `${daemonId.slice(0, 8)}...` : daemonId;
}

function capitalize(value: string): string {
  if (!value) return "Runtime";
  return `${value.slice(0, 1).toUpperCase()}${value.slice(1)}`;
}

function compareRuntimeMachines(a: RuntimeMachine, b: RuntimeMachine): number {
  if (a.isCurrent !== b.isCurrent) return a.isCurrent ? -1 : 1;
  const sectionDelta = sectionRank(a.section) - sectionRank(b.section);
  if (sectionDelta !== 0) return sectionDelta;
  if (a.onlineCount !== b.onlineCount) return b.onlineCount - a.onlineCount;
  return a.title.localeCompare(b.title);
}

function sectionRank(section: RuntimeMachineSection): number {
  switch (section) {
    case "local":
      return 0;
    case "remote":
      return 1;
    case "cloud":
      return 2;
  }
}
