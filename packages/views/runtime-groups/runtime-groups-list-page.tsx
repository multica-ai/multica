"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { RuntimeGroup, CreateRuntimeGroupRequest } from "@multica/core/types";
import { useNavigation } from "../navigation";
import { RuntimeGroupsPage } from "./runtime-groups-page";

export const runtimeGroupKeys = {
  list: (wsId: string) => ["runtime-groups", wsId] as const,
  detail: (id: string) => ["runtime-groups", "detail", id] as const,
};

export function RuntimeGroupsListPage() {
  const wsId = useWorkspaceId();
  const router = useNavigation();
  const paths = useWorkspacePaths();
  const qc = useQueryClient();
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);

  const { data: groups = [] } = useQuery({
    queryKey: runtimeGroupKeys.list(wsId),
    queryFn: () => api.listRuntimeGroups(wsId),
  });

  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const createGroup = useMutation({
    mutationFn: (req: CreateRuntimeGroupRequest) => api.createRuntimeGroup(req),
    onSuccess: () => qc.invalidateQueries({ queryKey: runtimeGroupKeys.list(wsId) }),
  });

  const handleCreate = async (req: { name: string; description: string; runtime_ids: string[] }) => {
    await createGroup.mutateAsync(req);
  };

  return (
    <RuntimeGroupsPage
      groups={groups as RuntimeGroup[]}
      runtimes={runtimes}
      members={members}
      currentUserId={currentUserId}
      onCreate={handleCreate}
      onOpenGroup={(id) => router.push(paths.runtimeGroupDetail(id))}
    />
  );
}
