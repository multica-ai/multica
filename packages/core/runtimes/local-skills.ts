import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type {
  CreateRuntimeLocalSkillImportRequest,
  RuntimeLocalSkillImportResult,
  RuntimeLocalSkillsResult,
} from "../types";

export const runtimeLocalSkillsKeys = {
  all: () => ["runtimes", "local-skills"] as const,
  forRuntime: (runtimeId: string) =>
    [...runtimeLocalSkillsKeys.all(), runtimeId] as const,
};

const POLL_INTERVAL_MS = 500;
const POLL_TIMEOUT_MS = 30_000;
// Import timeout is longer than discovery because old daemons (pre-batch) pop
// only one import per heartbeat cycle (~15s). With 10 queued imports the 10th
// can wait up to 150s in pending before being claimed, plus up to 60s for
// the daemon to actually run the import.
//
// Timeout invariant: IMPORT_POLL_TIMEOUT_MS must exceed
// runtimeLocalSkillPendingTimeout + runtimeLocalSkillRunningTimeout
// (server/internal/handler/runtime_local_skills.go).
// See also IMPORT_CONCURRENCY in packages/views/.../runtime-local-skill-import-panel.tsx
// and maxLocalSkillImportBatch in server/internal/handler/daemon.go.
const IMPORT_POLL_TIMEOUT_MS = 4 * 60_000; // 4 minutes

export async function resolveRuntimeLocalSkills(
  runtimeId: string,
): Promise<RuntimeLocalSkillsResult> {
  const initial = await api.initiateListLocalSkills(runtimeId);
  const start = Date.now();
  let current = initial;

  while (current.status === "pending" || current.status === "running") {
    if (Date.now() - start > POLL_TIMEOUT_MS) {
      throw new Error("runtime local skill discovery timed out");
    }
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
    current = await api.getListLocalSkillsResult(runtimeId, initial.id);
  }

  if (current.status === "failed" || current.status === "timeout") {
    throw new Error(current.error || "runtime local skill discovery failed");
  }

  return {
    skills: current.skills ?? [],
    supported: current.supported,
  };
}

export async function resolveRuntimeLocalSkillImport(
  runtimeId: string,
  payload: CreateRuntimeLocalSkillImportRequest,
): Promise<RuntimeLocalSkillImportResult> {
  const initial = await api.initiateImportLocalSkill(runtimeId, payload);
  const start = Date.now();
  let current = initial;

  while (current.status === "pending" || current.status === "running") {
    if (Date.now() - start > IMPORT_POLL_TIMEOUT_MS) {
      throw new Error("runtime local skill import timed out");
    }
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
    current = await api.getImportLocalSkillResult(runtimeId, initial.id);
  }

  if (current.status === "conflict") {
    if (!current.conflict) {
      throw new Error("runtime local skill import conflict missing details");
    }
    return {
      status: "conflict",
      conflict: current.conflict,
    };
  }

  if (current.status === "failed" || current.status === "timeout") {
    throw new Error(current.error || "runtime local skill import failed");
  }
  if (!current.skill) {
    throw new Error("runtime local skill import did not return a skill");
  }

  return {
    status: current.action === "overwrite" ? "updated" : "created",
    skill: current.skill,
  };
}

export function runtimeLocalSkillsOptions(runtimeId: string | null | undefined) {
  return queryOptions({
    queryKey: runtimeId
      ? runtimeLocalSkillsKeys.forRuntime(runtimeId)
      : runtimeLocalSkillsKeys.all(),
    queryFn: () => resolveRuntimeLocalSkills(runtimeId as string),
    enabled: Boolean(runtimeId),
    staleTime: 30_000,
    retry: false,
  });
}
