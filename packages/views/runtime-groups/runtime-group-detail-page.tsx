"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { workspaceKeys } from "@multica/core/workspace/queries";
import type { UpdateRuntimeGroupRequest, SetRuntimeGroupOverrideRequest } from "@multica/core/types";
import { useNavigation } from "../navigation";
import { RuntimeGroupDetail } from "./runtime-group-detail";
import { runtimeGroupKeys } from "./runtime-groups-list-page";

export function RuntimeGroupDetailPage({ groupId }: { groupId: string }) {
  const wsId = useWorkspaceId();
  const router = useNavigation();
  const paths = useWorkspacePaths();
  const qc = useQueryClient();

  const { data: group } = useQuery({
    queryKey: runtimeGroupKeys.detail(groupId),
    queryFn: () => api.getRuntimeGroup(groupId),
  });

  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));

  const updateGroup = useMutation({
    mutationFn: (updates: UpdateRuntimeGroupRequest) =>
      api.updateRuntimeGroup(groupId, updates),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeGroupKeys.detail(groupId) });
      qc.invalidateQueries({ queryKey: runtimeGroupKeys.list(wsId) });
    },
  });

  const deleteGroup = useMutation({
    mutationFn: () => api.deleteRuntimeGroup(groupId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeGroupKeys.list(wsId) });
      router.push(paths.runtimeGroups());
    },
  });

  const setOverride = useMutation({
    mutationFn: (req: SetRuntimeGroupOverrideRequest) =>
      api.setRuntimeGroupOverride(groupId, req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeGroupKeys.detail(groupId) });
      qc.invalidateQueries({ queryKey: runtimeGroupKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });

  const clearOverride = useMutation({
    mutationFn: () => api.clearRuntimeGroupOverride(groupId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: runtimeGroupKeys.detail(groupId) });
      qc.invalidateQueries({ queryKey: runtimeGroupKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });

  if (!group) return null;

  return (
    <RuntimeGroupDetail
      group={group}
      runtimes={runtimes}
      onUpdate={async (updates) => { await updateGroup.mutateAsync(updates); }}
      onDelete={async () => { await deleteGroup.mutateAsync(); }}
      onSetOverride={async (req) => { await setOverride.mutateAsync(req); }}
      onClearOverride={() => clearOverride.mutateAsync()}
    />
  );
}
