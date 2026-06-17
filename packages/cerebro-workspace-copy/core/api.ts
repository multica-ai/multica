import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";
import {
  CopyResultSchema,
  EMPTY_COPY_RESULT,
  RelinkResultSchema,
  type CopyResult,
  type CopyToWorkspaceInput,
  type RelinkResult,
} from "./types";

const copyPath = (wsId: string) =>
  `/api/workspaces/${wsId}/cerebro/copy`;

// copyEntity copies one entity from `wsId` into `input.targetWorkspaceId`.
// Non-destructive and idempotent on the backend; a re-copy returns the same
// target with already_copied = true.
export async function copyEntity(
  wsId: string,
  input: CopyToWorkspaceInput,
): Promise<CopyResult> {
  const raw = await api.cerebroRequest<unknown>(copyPath(wsId), {
    method: "POST",
    body: JSON.stringify({
      target_workspace_id: input.targetWorkspaceId,
      entity_type: input.entityType,
      source_id: input.sourceId,
    }),
  });
  return parseWithFallback(
    raw,
    CopyResultSchema,
    EMPTY_COPY_RESULT(input.entityType, input.sourceId),
    { endpoint: "POST /api/workspaces/:id/cerebro/copy" },
  );
}

// relinkIssues runs the target-only post-pass that heals copied issue->parent
// and issue->project links once both ends exist. Safe to call repeatedly.
export async function relinkIssues(
  wsId: string,
  targetWorkspaceId: string,
): Promise<RelinkResult> {
  const raw = await api.cerebroRequest<unknown>(copyPath(wsId), {
    method: "POST",
    body: JSON.stringify({
      target_workspace_id: targetWorkspaceId,
      entity_type: "relink",
    }),
  });
  return parseWithFallback(raw, RelinkResultSchema, {}, {
    endpoint: "POST /api/workspaces/:id/cerebro/copy (relink)",
  });
}
