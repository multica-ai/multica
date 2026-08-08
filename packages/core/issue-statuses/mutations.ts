import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueStatusKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import type { CreateIssueStatusRequest, UpdateIssueStatusRequest } from "../types";

/**
 * Catalog mutations (MUL-4809 §5). None of these patch optimistically: edits
 * happen in the settings tab where a round-trip is acceptable, and the server
 * canonicalizes the parts that matter (position, the one-default-per-Category
 * invariant, archive-with-migration).
 *
 * Every mutation invalidates the ISSUE caches too, because issue rows embed
 * `status_detail` — a rename/recolor changes how existing rows render, and an
 * archive can move issues to another status entirely.
 */
function useCatalogInvalidation() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return () => {
    qc.invalidateQueries({ queryKey: issueStatusKeys.all(wsId) });
    qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
  };
}

export function useCreateIssueStatus() {
  const invalidate = useCatalogInvalidation();
  return useMutation({
    mutationFn: (data: CreateIssueStatusRequest) => api.createIssueStatus(data),
    onSettled: invalidate,
  });
}

export function useUpdateIssueStatus() {
  const invalidate = useCatalogInvalidation();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateIssueStatusRequest) =>
      api.updateIssueStatus(id, data),
    onSettled: invalidate,
  });
}

/**
 * Archive a custom status. `migrateToStatusId` is required by the server when
 * the status still has issues; it must name a non-archived status in the SAME
 * Category, and the issues move over in the same transaction.
 */
export function useArchiveIssueStatus() {
  const invalidate = useCatalogInvalidation();
  return useMutation({
    mutationFn: ({ id, migrateToStatusId }: { id: string; migrateToStatusId?: string }) =>
      api.archiveIssueStatus(id, migrateToStatusId),
    onSettled: invalidate,
  });
}
