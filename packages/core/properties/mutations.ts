import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { propertyKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import { onIssuePropertiesChanged } from "../issues/ws-updaters";
import type {
  CreatePropertyRequest,
  UpdatePropertyRequest,
  Issue,
  IssuePropertyValue,
  IssuePropertyValues,
} from "../types";

export function useCreateProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreatePropertyRequest) => api.createProperty(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
    },
  });
}

/**
 * Definition updates (rename, options, archive). No optimistic patch: edits
 * happen in the settings dialog where a round-trip is acceptable, and config
 * canonicalization (option id assignment) is server-side anyway.
 */
export function useUpdateProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdatePropertyRequest) =>
      api.updateProperty(id, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
      // Issue caches embed the value bag; a definition change (rename,
      // option edits) changes how values render, and rows referencing an
      // archived definition must drop out of "+ Add property" menus.
      qc.invalidateQueries({ queryKey: issueKeys.all(wsId) });
    },
  });
}

function readIssueProperties(qc: ReturnType<typeof useQueryClient>, wsId: string, issueId: string): IssuePropertyValues | undefined {
  return qc.getQueryData<Issue>(issueKeys.detail(wsId, issueId))?.properties;
}

/**
 * Optimistic single-property write on an issue (canonical toggle/field-patch
 * pattern per the state rules): patch the caches through the same helper the
 * WS event uses, roll back on failure, reconcile with the server's
 * post-mutation bag on success.
 */
export function useSetIssueProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ issueId, propertyId, value }: { issueId: string; propertyId: string; value: IssuePropertyValue }) =>
      api.setIssueProperty(issueId, propertyId, value),
    onMutate: async ({ issueId, propertyId, value }) => {
      await qc.cancelQueries({ queryKey: issueKeys.detail(wsId, issueId) });
      const prev = readIssueProperties(qc, wsId, issueId);
      onIssuePropertiesChanged(qc, wsId, issueId, { ...(prev ?? {}), [propertyId]: value });
      return { prev, issueId };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined) onIssuePropertiesChanged(qc, wsId, ctx.issueId, ctx.prev);
    },
    onSuccess: (data, { issueId }) => {
      onIssuePropertiesChanged(qc, wsId, issueId, data.properties ?? {});
    },
  });
}

export function useUnsetIssueProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ issueId, propertyId }: { issueId: string; propertyId: string }) =>
      api.unsetIssueProperty(issueId, propertyId),
    onMutate: async ({ issueId, propertyId }) => {
      await qc.cancelQueries({ queryKey: issueKeys.detail(wsId, issueId) });
      const prev = readIssueProperties(qc, wsId, issueId);
      if (prev) {
        const next = { ...prev };
        delete next[propertyId];
        onIssuePropertiesChanged(qc, wsId, issueId, next);
      }
      return { prev, issueId };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined) onIssuePropertiesChanged(qc, wsId, ctx.issueId, ctx.prev);
    },
    onSuccess: (data, { issueId }) => {
      onIssuePropertiesChanged(qc, wsId, issueId, data.properties ?? {});
    },
  });
}
