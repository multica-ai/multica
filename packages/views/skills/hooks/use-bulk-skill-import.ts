import { useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { CreateSkillRequest, Skill } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  skillDetailOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { isNameConflictError } from "../lib/utils";

const IMPORT_CONCURRENCY = 10;

export type BulkTask =
  | { key: string; name: string; kind: "payload"; data: CreateSkillRequest }
  | { key: string; name: string; kind: "url"; url: string; importName: string };

export type BulkResult = {
  key: string;
  name: string;
  status: "success" | "skipped" | "failed";
  error?: string;
  skill?: Skill;
};

export type BulkPhase = "idle" | "importing" | "done" | "cancelled";

type Deps = {
  createSkill: (d: CreateSkillRequest) => Promise<Skill>;
  importSkill: (d: { url: string }) => Promise<Skill>;
  onProgress: (r: BulkResult[]) => void;
};

// Pure runner — testable without React. 10-wide pool; partial-success.
export async function runBulkImport(
  tasks: BulkTask[],
  deps: Deps,
  cancelRef: { current: boolean },
): Promise<BulkResult[]> {
  const results: BulkResult[] = [];

  const importOne = async (task: BulkTask) => {
    try {
      const skill =
        task.kind === "payload"
          ? await deps.createSkill(task.data)
          : await deps.importSkill({ url: task.url });
      results.push({ key: task.key, name: task.name, status: "success", skill });
    } catch (err) {
      const msg = err instanceof Error ? err.message : "";
      results.push({
        key: task.key,
        name: task.name,
        status: isNameConflictError(msg) ? "skipped" : "failed",
        error: msg,
      });
    }
    deps.onProgress([...results]);
  };

  const executing = new Set<Promise<void>>();
  for (const task of tasks) {
    if (cancelRef.current) break;
    const p = importOne(task).then(() => {
      executing.delete(p);
    });
    executing.add(p);
    if (executing.size >= IMPORT_CONCURRENCY) {
      await Promise.race(executing);
    }
  }
  await Promise.all(executing);
  return results;
}

// React wrapper — owns phase/progress state + query invalidation.
export function useBulkSkillImport() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const cancelRef = useRef(false);
  const [phase, setPhase] = useState<BulkPhase>("idle");
  const [total, setTotal] = useState(0);
  const [results, setResults] = useState<BulkResult[]>([]);

  const start = async (tasks: BulkTask[]) => {
    cancelRef.current = false;
    setTotal(tasks.length);
    setResults([]);
    setPhase("importing");

    const finalResults = await runBulkImport(
      tasks,
      {
        createSkill: api.createSkill.bind(api),
        importSkill: api.importSkill.bind(api),
        onProgress: setResults,
      },
      cancelRef,
    );

    await Promise.all([
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId) }),
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) }),
    ]);
    for (const r of finalResults) {
      if (r.status === "success" && r.skill) {
        qc.setQueryData(
          skillDetailOptions(wsId, r.skill.id).queryKey,
          r.skill,
        );
      }
    }
    setPhase(cancelRef.current ? "cancelled" : "done");
  };

  const cancel = () => {
    cancelRef.current = true;
  };

  return { phase, total, results, completed: results.length, start, cancel };
}
