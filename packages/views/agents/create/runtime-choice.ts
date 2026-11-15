import {
  isRuntimeUsableForUser,
  providerDisplayName,
} from "@multica/core/runtimes";
import type { RuntimeDevice } from "@multica/core/types";
import {
  runtimeRowLabel,
  type RuntimeMachine,
} from "../../runtimes/components/runtime-machines";

export const QODER_PROVIDER = "qoder_cloud";

/** Harness rows shown under an open machine before the rest fold into one row. */
export const HARNESS_PREVIEW_COUNT = 6;
/** Provider logos on a closed machine row before "+N". */
export const LOGO_STACK_LIMIT = 4;

export interface HarnessOption {
  runtime: RuntimeDevice;
  label: string;
  version: string | null;
  custom: boolean;
}

export interface DeviceOption {
  machine: RuntimeMachine;
  harnesses: HarnessOption[];
}

export interface RuntimeChoiceGroups {
  /** This device, or null when the client cannot run harnesses (web). */
  local: DeviceOption | null;
  remotes: DeviceOption[];
  /** Remote devices with no harness this member may use. */
  hiddenRemoteCount: number;
  /** Managed cloud runtimes other than Qoder, which has its own tile. */
  cloud: HarnessOption[];
  qoderRuntime: RuntimeDevice | null;
}

/**
 * The version number out of a CLI's `--version` output, which comes in many
 * shapes ("codex-cli 0.161.0", "kimi, version 1.37.0", "2.1.300 (Claude Code)").
 */
export function shortVersion(raw: string): string {
  return raw.match(/\d+(?:\.\d+)+(?:-[\w.]+)?/)?.[0] ?? raw;
}

export function harnessOption(
  runtime: RuntimeDevice,
  machineTitle: string,
): HarnessOption {
  const version =
    typeof runtime.metadata?.version === "string" && runtime.metadata.version
      ? shortVersion(runtime.metadata.version)
      : null;
  return {
    runtime,
    label:
      runtime.runtime_mode === "cloud"
        ? providerDisplayName(runtime.provider)
        : runtimeRowLabel(runtime, machineTitle),
    version,
    custom: !!runtime.profile_id,
  };
}

function byLabel(a: HarnessOption, b: HarnessOption): number {
  return a.label.localeCompare(b.label);
}

export function buildRuntimeChoiceGroups(
  machines: RuntimeMachine[],
  currentUserId: string | null,
): RuntimeChoiceGroups {
  let local: DeviceOption | null = null;
  const remotes: DeviceOption[] = [];
  let hiddenRemoteCount = 0;
  const cloud: HarnessOption[] = [];
  let qoderRuntime: RuntimeDevice | null = null;

  for (const machine of machines) {
    const usable = machine.runtimes.filter((runtime) =>
      isRuntimeUsableForUser(runtime, currentUserId),
    );
    if (machine.section === "local") {
      local = {
        machine,
        harnesses: usable
          .map((runtime) => harnessOption(runtime, machine.title))
          .sort(byLabel),
      };
      continue;
    }
    if (machine.section === "cloud") {
      for (const runtime of usable) {
        if (runtime.provider === QODER_PROVIDER) {
          if (!qoderRuntime || runtime.status === "online") qoderRuntime = runtime;
          continue;
        }
        cloud.push(harnessOption(runtime, machine.title));
      }
      continue;
    }
    if (usable.length === 0) {
      hiddenRemoteCount += 1;
      continue;
    }
    remotes.push({
      machine,
      harnesses: usable
        .map((runtime) => harnessOption(runtime, machine.title))
        .sort(byLabel),
    });
  }

  return { local, remotes, hiddenRemoteCount, cloud: cloud.sort(byLabel), qoderRuntime };
}

export function normalizeQuery(query: string): string {
  return query.trim().toLowerCase();
}

/** A harness matches by its own name or provider, or by its device's name. */
export function harnessMatches(
  option: HarnessOption,
  deviceTitle: string,
  query: string,
): boolean {
  const q = normalizeQuery(query);
  if (!q) return true;
  return [option.label, option.runtime.provider, providerDisplayName(option.runtime.provider), deviceTitle]
    .join(" ")
    .toLowerCase()
    .includes(q);
}

export function filterDevice(
  device: DeviceOption,
  query: string,
): DeviceOption | null {
  const harnesses = device.harnesses.filter((option) =>
    harnessMatches(option, device.machine.title, query),
  );
  return harnesses.length > 0 ? { ...device, harnesses } : null;
}

/**
 * The first `limit` options, with the selected one swapped into the last slot
 * when it would otherwise be hidden — a collapsed list must never hide what is
 * selected. A single leftover is shown rather than folded, since the fold row
 * would take the same space.
 */
export function previewOptions(
  options: HarnessOption[],
  selectedRuntimeId: string | null,
  limit: number,
): { shown: HarnessOption[]; hiddenCount: number } {
  if (options.length <= limit + 1) return { shown: options, hiddenCount: 0 };
  const shown = options.slice(0, limit);
  const selected = selectedRuntimeId
    ? options.find((option) => option.runtime.id === selectedRuntimeId)
    : undefined;
  if (selected && !shown.includes(selected)) {
    shown[shown.length - 1] = selected;
  }
  return { shown, hiddenCount: options.length - shown.length };
}
