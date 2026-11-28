/**
 * Smart-runtime recommended config is a CLI (cursor/codex/…), not a machine.
 * Empty `runtime_id` on a smart agent is the stored convention, not unbind.
 */

const PREFERRED_RUNTIME_CLIS = ["cursor", "codex"] as const;

export type RuntimeCliSource = {
  id: string;
  provider: string;
  status: string;
  owner_id: string | null;
  visibility?: string;
};

export function normalizeRuntimeCli(value: string | null | undefined): string {
  return value?.trim().toLowerCase() ?? "";
}

export function recommendedRuntimeCli(
  agent: {
    cli?: string;
    runtime_config_summary?: { recommended?: { cli?: string } };
  },
  boundRuntime: Pick<RuntimeCliSource, "provider"> | null | undefined,
): string {
  return (
    normalizeRuntimeCli(agent.cli) ||
    normalizeRuntimeCli(agent.runtime_config_summary?.recommended?.cli) ||
    normalizeRuntimeCli(boundRuntime?.provider)
  );
}

export function uniqueRuntimeClis(runtimes: RuntimeCliSource[]): string[] {
  const seen = new Set<string>();
  for (const runtime of runtimes) {
    const cli = normalizeRuntimeCli(runtime.provider);
    if (cli) seen.add(cli);
  }
  return [...seen].sort(compareRuntimeClis);
}

function compareRuntimeClis(a: string, b: string): number {
  const ai = PREFERRED_RUNTIME_CLIS.indexOf(
    a as (typeof PREFERRED_RUNTIME_CLIS)[number],
  );
  const bi = PREFERRED_RUNTIME_CLIS.indexOf(
    b as (typeof PREFERRED_RUNTIME_CLIS)[number],
  );
  if (ai !== -1 || bi !== -1) {
    if (ai === -1) return 1;
    if (bi === -1) return -1;
    return ai - bi;
  }
  return a.localeCompare(b);
}

/**
 * Local catalog lookup for model discovery. Never written back as
 * recommended `runtime_id`.
 */
export function catalogRuntimeForCli<T extends RuntimeCliSource>(
  runtimes: T[],
  cli: string,
  currentUserId: string | null,
): T | null {
  const wanted = normalizeRuntimeCli(cli);
  if (!wanted) return null;
  const matches = runtimes.filter(
    (runtime) => normalizeRuntimeCli(runtime.provider) === wanted,
  );
  if (matches.length === 0) return null;
  const mine = currentUserId
    ? matches.filter((runtime) => runtime.owner_id === currentUserId)
    : [];
  return (
    mine.find((runtime) => runtime.status === "online") ??
    matches.find((runtime) => runtime.status === "online") ??
    mine[0] ??
    matches[0] ??
    null
  );
}

/** Smart recommended save: CLI only. Empty `runtime_id` is not sent. */
export function buildSmartRecommendedRuntimeUpdate(cli: string): {
  cli: string;
  model: string;
  thinking_level: string;
  service_tier: string;
} {
  return {
    cli: normalizeRuntimeCli(cli),
    model: "",
    thinking_level: "",
    service_tier: "",
  };
}

/**
 * Smart agents store an empty `runtime_id`. Switching to fixed must send a
 * concrete machine — never that empty id.
 */
export function pickFixedRuntimeIdForSmartSwitch<T extends RuntimeCliSource>(options: {
  currentRuntimeId?: string | null;
  recommendedCli: string;
  runtimes: T[];
  currentUserId: string | null;
  isUsable: (runtime: T) => boolean;
}): string | null {
  const usable = options.runtimes.filter(options.isUsable);
  const currentId = options.currentRuntimeId?.trim();
  if (currentId && usable.some((runtime) => runtime.id === currentId)) {
    return currentId;
  }
  const catalog = catalogRuntimeForCli(
    usable,
    options.recommendedCli,
    options.currentUserId,
  );
  if (catalog) return catalog.id;
  const mine = options.currentUserId
    ? usable.filter((runtime) => runtime.owner_id === options.currentUserId)
    : [];
  return (
    mine.find((runtime) => runtime.status === "online")?.id ??
    usable.find((runtime) => runtime.status === "online")?.id ??
    mine[0]?.id ??
    usable[0]?.id ??
    null
  );
}
