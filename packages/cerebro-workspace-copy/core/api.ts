import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";
import {
  CopyResultSchema,
  EMPTY_COPY_RESULT,
  FoundationResultSchema,
  RelinkResultSchema,
  type CopyResult,
  type CopyToWorkspaceInput,
  type FoundationResult,
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
      cascade: input.cascade ?? false,
      conflict: input.conflict ?? "skip",
    }),
  });
  return parseWithFallback(
    raw,
    CopyResultSchema,
    EMPTY_COPY_RESULT(input.entityType, input.sourceId),
    { endpoint: "POST /api/workspaces/:id/cerebro/copy" },
  );
}

// copyFoundation runs the one-time foundation bulk pass (labels, skills,
// connections + credentials, roles/groups/permissions, GitHub, settings) — the
// "do this first" step before any per-item copy.
export async function copyFoundation(
  wsId: string,
  targetWorkspaceId: string,
): Promise<FoundationResult> {
  const raw = await api.cerebroRequest<unknown>(copyPath(wsId), {
    method: "POST",
    body: JSON.stringify({
      target_workspace_id: targetWorkspaceId,
      entity_type: "foundation",
    }),
  });
  return parseWithFallback(raw, FoundationResultSchema, {}, {
    endpoint: "POST /api/workspaces/:id/cerebro/copy (foundation)",
  });
}

// copyWorkspaceArtifacts bulk-copies the workspace's folder tree + its
// workspace-scoped documents and notes into the target.
export async function copyWorkspaceArtifacts(
  wsId: string,
  targetWorkspaceId: string,
): Promise<CopyResult> {
  const raw = await api.cerebroRequest<unknown>(copyPath(wsId), {
    method: "POST",
    body: JSON.stringify({
      target_workspace_id: targetWorkspaceId,
      entity_type: "workspace_artifacts",
    }),
  });
  return parseWithFallback(
    raw,
    CopyResultSchema,
    EMPTY_COPY_RESULT("workspace_artifacts", ""),
    { endpoint: "POST /api/workspaces/:id/cerebro/copy (workspace_artifacts)" },
  );
}

// relinkGroupAccess heals group→agent access grants after the agents are copied.
export async function relinkGroupAccess(
  wsId: string,
  targetWorkspaceId: string,
): Promise<{ group_agent_access_relinked?: number }> {
  const raw = await api.cerebroRequest<{ group_agent_access_relinked?: number }>(
    copyPath(wsId),
    {
      method: "POST",
      body: JSON.stringify({
        target_workspace_id: targetWorkspaceId,
        entity_type: "group_access",
      }),
    },
  );
  return raw ?? {};
}

// relinkIssues runs the target-only post-pass that heals copied issue->parent
// and issue->project links once both ends exist, and rewrites internal
// references across issues, comments, agents, skills, autopilots and chats.
// Safe to call repeatedly.
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
