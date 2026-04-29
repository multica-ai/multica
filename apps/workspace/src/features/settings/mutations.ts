import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  CreateMemberRequest,
  CreatePersonalAccessTokenResponse,
  MemberWithUser,
  PersonalAccessToken,
  UpdateMeRequest,
  UpdateMemberRequest,
  User,
  Workspace,
  WorkspaceRepo,
} from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useAuthStore } from "@/features/auth";
import { useWorkspaceStore } from "@/features/workspace";

interface UpdateWorkspaceInput {
  name?: string;
  description?: string;
  context?: string;
  settings?: Record<string, unknown>;
  repos?: WorkspaceRepo[];
}

export function useAccountMutations() {
  const queryClient = useQueryClient();

  const updateMeMutation = useMutation({
    mutationFn: (data: UpdateMeRequest) => api.updateMe(data),
    onSuccess: (user) => {
      useAuthStore.getState().setUser(user);
      queryClient.setQueryData<User | null>(queryKeys.session.me(), user);
    },
  });

  return {
    updateMe: (data: UpdateMeRequest) => updateMeMutation.mutateAsync(data),
    updating: updateMeMutation.isPending,
  };
}

export function useWorkspaceSettingsMutations() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id ?? null);

  const updateWorkspaceMutation = useMutation({
    mutationFn: async (data: UpdateWorkspaceInput) => {
      if (!workspaceId) {
        throw new Error("No workspace selected");
      }

      return api.updateWorkspace(workspaceId, data);
    },
    onSuccess: (workspace) => {
      useWorkspaceStore.getState().updateWorkspace(workspace);
      queryClient.setQueryData<Workspace[]>(queryKeys.workspaces.all(), (existing = []) =>
        existing.map((item) => (item.id === workspace.id ? workspace : item)),
      );
    },
  });

  const createMemberMutation = useMutation({
    mutationFn: async (data: CreateMemberRequest) => {
      if (!workspaceId) {
        throw new Error("No workspace selected");
      }

      return api.createMember(workspaceId, data);
    },
    onSuccess: () => {
      if (!workspaceId) return;
      void queryClient.invalidateQueries({ queryKey: queryKeys.workspace.members(workspaceId) });
    },
  });

  const updateMemberMutation = useMutation({
    mutationFn: async ({ memberId, data }: { memberId: string; data: UpdateMemberRequest }) => {
      if (!workspaceId) {
        throw new Error("No workspace selected");
      }

      return api.updateMember(workspaceId, memberId, data);
    },
    onSuccess: (member) => {
      if (!workspaceId) return;
      queryClient.setQueryData<MemberWithUser[]>(queryKeys.workspace.members(workspaceId), (existing = []) =>
        existing.map((item) => (item.id === member.id ? member : item)),
      );
    },
  });

  const deleteMemberMutation = useMutation({
    mutationFn: async (memberId: string) => {
      if (!workspaceId) {
        throw new Error("No workspace selected");
      }

      await api.deleteMember(workspaceId, memberId);
      return memberId;
    },
    onSuccess: (memberId) => {
      if (!workspaceId) return;
      queryClient.setQueryData<MemberWithUser[]>(queryKeys.workspace.members(workspaceId), (existing = []) =>
        existing.filter((member) => member.id !== memberId),
      );
    },
  });

  return {
    updateWorkspace: (data: UpdateWorkspaceInput) => updateWorkspaceMutation.mutateAsync(data),
    createMember: (data: CreateMemberRequest) => createMemberMutation.mutateAsync(data),
    updateMember: (memberId: string, data: UpdateMemberRequest) =>
      updateMemberMutation.mutateAsync({ memberId, data }),
    deleteMember: (memberId: string) => deleteMemberMutation.mutateAsync(memberId),
  };
}

export function usePersonalAccessTokenMutations() {
  const queryClient = useQueryClient();

  const createTokenMutation = useMutation({
    mutationFn: (data: { name: string; expires_in_days?: number }) => api.createPersonalAccessToken(data),
    onSuccess: (token) => {
      queryClient.setQueryData<PersonalAccessToken[]>(queryKeys.settings.tokens(), (existing = []) => {
        const nextToken: PersonalAccessToken = {
          id: token.id,
          name: token.name,
          token_prefix: token.token_prefix,
          expires_at: token.expires_at,
          last_used_at: token.last_used_at,
          created_at: token.created_at,
        };
        return [nextToken, ...existing];
      });
    },
  });

  const revokeTokenMutation = useMutation({
    mutationFn: (id: string) => api.revokePersonalAccessToken(id),
    onSuccess: (_result, id) => {
      queryClient.setQueryData<PersonalAccessToken[]>(queryKeys.settings.tokens(), (existing = []) =>
        existing.filter((token) => token.id !== id),
      );
    },
  });

  return {
    createToken: (data: { name: string; expires_in_days?: number }) =>
      createTokenMutation.mutateAsync(data),
    revokeToken: (id: string) => revokeTokenMutation.mutateAsync(id),
    creating: createTokenMutation.isPending,
    revokingId: revokeTokenMutation.isPending
      ? revokeTokenMutation.variables ?? null
      : null,
  };
}
