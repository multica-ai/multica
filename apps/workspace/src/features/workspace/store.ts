"use client";

import { useMemo } from "react";
import { create } from "zustand";
import { toast } from "sonner";
import type { Agent, MemberWithUser, Skill, Workspace } from "@/shared/types";
import { useRuntimeStore } from "@/features/runtimes";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";
import {
  clearWorkspaceScopedQueryCaches,
  getAppQueryClient,
  prepareQueryCacheForWorkspaceSwitch,
  queryKeys,
} from "@/shared/query";
import {
  useWorkspaceAgentsQuery,
  useWorkspaceMembersQuery,
  useWorkspaceSkillsQuery,
  useWorkspacesQuery,
  workspaceAgentsQueryOptions,
  workspaceMembersQueryOptions,
  workspaceSkillsQueryOptions,
  workspacesQueryOptions,
} from "./queries";
import { inboxQueryOptions } from "@/features/inbox/queries";
import { issuesListQueryOptions } from "@/features/issues/queries";
import { runtimesQueryOptions } from "@/features/runtimes/queries";

const logger = createLogger("workspace-store");

interface WorkspaceRemoteState {
  workspace: Workspace | null;
  workspaces: Workspace[];
  members: MemberWithUser[];
  agents: Agent[];
  skills: Skill[];
}

interface WorkspaceLocalState {
  currentWorkspaceId: string | null;
}

interface WorkspaceActions {
  hydrateWorkspace: (
    wsList: Workspace[],
    preferredWorkspaceId?: string | null,
  ) => Promise<Workspace | null>;
  switchWorkspace: (workspaceId: string) => Promise<void>;
  refreshWorkspaces: () => Promise<Workspace[]>;
  refreshMembers: () => Promise<void>;
  updateAgent: (id: string, updates: Partial<Agent>) => void;
  refreshAgents: () => Promise<void>;
  refreshSkills: () => Promise<void>;
  upsertSkill: (skill: Skill) => void;
  removeSkill: (id: string) => void;
  createWorkspace: (data: {
    name: string;
    slug: string;
    description?: string;
  }) => Promise<Workspace>;
  updateWorkspace: (ws: Workspace) => void;
  leaveWorkspace: (workspaceId: string) => Promise<void>;
  deleteWorkspace: (workspaceId: string) => Promise<void>;
  clearWorkspace: () => void;
}

type WorkspaceStore = WorkspaceRemoteState & WorkspaceLocalState & WorkspaceActions;
type WorkspaceStoreSelector<T> = (state: WorkspaceStore) => T;

interface WorkspaceStoreHook {
  <T>(selector: WorkspaceStoreSelector<T>): T;
  getState: () => WorkspaceStore;
  subscribe: (listener: (state: WorkspaceStore) => void) => () => void;
}

function resolveWorkspaceId(
  currentWorkspaceId: string | null,
  workspaces: Workspace[],
): string | null {
  const storedWorkspaceId =
    typeof window !== "undefined"
      ? localStorage.getItem("multica_workspace_id")
      : null;

  if (currentWorkspaceId && workspaces.some((item) => item.id === currentWorkspaceId)) {
    return currentWorkspaceId;
  }

  if (storedWorkspaceId && workspaces.some((item) => item.id === storedWorkspaceId)) {
    return storedWorkspaceId;
  }

  return workspaces[0]?.id ?? null;
}

async function primeWorkspaceQueries(workspaceId: string) {
  const queryClient = getAppQueryClient();

  await Promise.all([
    queryClient.fetchQuery(workspaceMembersQueryOptions(workspaceId)),
    queryClient.fetchQuery(workspaceAgentsQueryOptions(workspaceId)),
    queryClient.fetchQuery(workspaceSkillsQueryOptions(workspaceId)),
    queryClient.fetchQuery(issuesListQueryOptions(workspaceId)).catch(() => ({ issues: [], total: 0 })),
    queryClient.fetchQuery(inboxQueryOptions(workspaceId)).catch(() => []),
    queryClient.fetchQuery(runtimesQueryOptions(workspaceId)).catch(() => []),
  ]);
}

function getWorkspaceSnapshot(): WorkspaceStore {
  const queryClient = getAppQueryClient();
  const localState = useWorkspaceSessionStore.getState();
  const workspaces =
    queryClient.getQueryData<Workspace[]>(queryKeys.workspaces.all()) ?? [];
  const currentWorkspaceId = resolveWorkspaceId(
    localState.currentWorkspaceId,
    workspaces,
  );
  const workspace =
    workspaces.find((item) => item.id === currentWorkspaceId) ?? null;
  const members = workspace
    ? queryClient.getQueryData<MemberWithUser[]>(
        queryKeys.workspace.members(workspace.id),
      ) ?? []
    : [];
  const agents = workspace
    ? queryClient.getQueryData<Agent[]>(queryKeys.workspace.agents(workspace.id)) ?? []
    : [];
  const skills = workspace
    ? queryClient.getQueryData<Skill[]>(queryKeys.workspace.skills(workspace.id)) ?? []
    : [];

  return {
    ...localState,
    currentWorkspaceId,
    workspace,
    workspaces,
    members,
    agents,
    skills,
  };
}

const useWorkspaceSessionStore = create<WorkspaceLocalState & WorkspaceActions>(
  (set, get) => ({
    currentWorkspaceId: null,

    hydrateWorkspace: async (wsList, preferredWorkspaceId) => {
      const queryClient = getAppQueryClient();
      queryClient.setQueryData(queryKeys.workspaces.all(), wsList);

      const nextWorkspace =
        (preferredWorkspaceId
          ? wsList.find((item) => item.id === preferredWorkspaceId)
          : null) ??
        wsList[0] ??
        null;

      if (!nextWorkspace) {
        void clearWorkspaceScopedQueryCaches(queryClient);
        api.setWorkspaceId(null);
        localStorage.removeItem("multica_workspace_id");
        set({ currentWorkspaceId: null });
        return null;
      }

      api.setWorkspaceId(nextWorkspace.id);
      localStorage.setItem("multica_workspace_id", nextWorkspace.id);
      set({ currentWorkspaceId: nextWorkspace.id });

      logger.debug("hydrate workspace", nextWorkspace.name, nextWorkspace.id);

      try {
        await primeWorkspaceQueries(nextWorkspace.id);
      } catch (error) {
        logger.error("failed to prime workspace queries", error);
      }

      return nextWorkspace;
    },

    switchWorkspace: async (workspaceId) => {
      logger.info("switching to", workspaceId);
      const { workspaces, workspace: currentWorkspace } = getWorkspaceSnapshot();
      const nextWorkspace = workspaces.find((item) => item.id === workspaceId);
      if (!nextWorkspace) return;

      await prepareQueryCacheForWorkspaceSwitch(
        getAppQueryClient(),
        currentWorkspace?.id,
      );

      api.setWorkspaceId(nextWorkspace.id);
      localStorage.setItem("multica_workspace_id", nextWorkspace.id);
      set({ currentWorkspaceId: nextWorkspace.id });

      useRuntimeStore.getState().setRuntimes([]);

      await get().hydrateWorkspace(workspaces, nextWorkspace.id);
    },

    refreshWorkspaces: async () => {
      const queryClient = getAppQueryClient();
      const { workspace } = getWorkspaceSnapshot();
      const storedWorkspaceId = localStorage.getItem("multica_workspace_id");

      try {
        const wsList = await queryClient.fetchQuery(workspacesQueryOptions());
        await get().hydrateWorkspace(
          wsList,
          workspace?.id ?? storedWorkspaceId,
        );
        return wsList;
      } catch (error) {
        logger.error("failed to refresh workspaces", error);
        toast.error("Failed to refresh workspaces");
        return getWorkspaceSnapshot().workspaces;
      }
    },

    refreshMembers: async () => {
      const { workspace } = getWorkspaceSnapshot();
      if (!workspace) return;

      try {
        await getAppQueryClient().fetchQuery(
          workspaceMembersQueryOptions(workspace.id),
        );
      } catch (error) {
        logger.error("failed to refresh members", error);
        toast.error("Failed to load members");
      }
    },

    updateAgent: (id, updates) => {
      const { workspace } = getWorkspaceSnapshot();
      if (!workspace) return;

      getAppQueryClient().setQueryData<Agent[]>(
        queryKeys.workspace.agents(workspace.id),
        (existing = []) =>
          existing.map((agent) =>
            agent.id === id ? { ...agent, ...updates } : agent,
          ),
      );
    },

    refreshAgents: async () => {
      const { workspace } = getWorkspaceSnapshot();
      if (!workspace) return;

      try {
        await getAppQueryClient().fetchQuery(
          workspaceAgentsQueryOptions(workspace.id),
        );
      } catch (error) {
        logger.error("failed to refresh agents", error);
        toast.error("Failed to load agents");
      }
    },

    refreshSkills: async () => {
      const { workspace } = getWorkspaceSnapshot();
      if (!workspace) return;

      try {
        await getAppQueryClient().fetchQuery(
          workspaceSkillsQueryOptions(workspace.id),
        );
      } catch (error) {
        logger.error("failed to refresh skills", error);
        toast.error("Failed to load skills");
      }
    },

    upsertSkill: (skill) => {
      const { workspace } = getWorkspaceSnapshot();
      if (!workspace) return;

      getAppQueryClient().setQueryData<Skill[]>(
        queryKeys.workspace.skills(workspace.id),
        (existing = []) => {
          const idx = existing.findIndex((item) => item.id === skill.id);
          if (idx >= 0) {
            const next = [...existing];
            next[idx] = skill;
            return next;
          }

          return [...existing, skill];
        },
      );
    },

    removeSkill: (id) => {
      const { workspace } = getWorkspaceSnapshot();
      if (!workspace) return;

      getAppQueryClient().setQueryData<Skill[]>(
        queryKeys.workspace.skills(workspace.id),
        (existing = []) => existing.filter((skill) => skill.id !== id),
      );
    },

    createWorkspace: async (data) => {
      const workspace = await api.createWorkspace(data);
      getAppQueryClient().setQueryData<Workspace[]>(
        queryKeys.workspaces.all(),
        (existing = []) => [...existing, workspace],
      );
      return workspace;
    },

    updateWorkspace: (workspace) => {
      getAppQueryClient().setQueryData<Workspace[]>(
        queryKeys.workspaces.all(),
        (existing = []) =>
          existing.map((item) =>
            item.id === workspace.id ? workspace : item,
          ),
      );
    },

    leaveWorkspace: async (workspaceId) => {
      await api.leaveWorkspace(workspaceId);
      const wsList = await getAppQueryClient().fetchQuery(workspacesQueryOptions());
      const { workspace } = getWorkspaceSnapshot();
      const preferredWorkspaceId =
        workspace?.id === workspaceId ? null : workspace?.id ?? null;
      await get().hydrateWorkspace(wsList, preferredWorkspaceId);
    },

    deleteWorkspace: async (workspaceId) => {
      await api.deleteWorkspace(workspaceId);
      const wsList = await getAppQueryClient().fetchQuery(workspacesQueryOptions());
      const { workspace } = getWorkspaceSnapshot();
      const preferredWorkspaceId =
        workspace?.id === workspaceId ? null : workspace?.id ?? null;
      await get().hydrateWorkspace(wsList, preferredWorkspaceId);
    },

    clearWorkspace: () => {
      void clearWorkspaceScopedQueryCaches(getAppQueryClient());
      api.setWorkspaceId(null);
      localStorage.removeItem("multica_workspace_id");
      set({ currentWorkspaceId: null });
    },
  }),
);

export const useWorkspaceStore = ((selector: WorkspaceStoreSelector<unknown>) => {
  const sessionState = useWorkspaceSessionStore();
  const workspacesQuery = useWorkspacesQuery();
  const workspaces = workspacesQuery.data ?? [];
  const currentWorkspaceId = resolveWorkspaceId(
    sessionState.currentWorkspaceId,
    workspaces,
  );
  const workspace =
    workspaces.find((item) => item.id === currentWorkspaceId) ?? null;
  const membersQuery = useWorkspaceMembersQuery(workspace?.id ?? null);
  const agentsQuery = useWorkspaceAgentsQuery(workspace?.id ?? null);
  const skillsQuery = useWorkspaceSkillsQuery(workspace?.id ?? null);

  const snapshot = useMemo<WorkspaceStore>(
    () => ({
      ...sessionState,
      currentWorkspaceId,
      workspace,
      workspaces,
      members: membersQuery.data ?? [],
      agents: agentsQuery.data ?? [],
      skills: skillsQuery.data ?? [],
    }),
    [
      agentsQuery.data,
      currentWorkspaceId,
      membersQuery.data,
      sessionState,
      skillsQuery.data,
      workspace,
      workspaces,
    ],
  );

  return selector(snapshot);
}) as WorkspaceStoreHook;

useWorkspaceStore.getState = getWorkspaceSnapshot;

useWorkspaceStore.subscribe = (listener) =>
  useWorkspaceSessionStore.subscribe(() => {
    listener(getWorkspaceSnapshot());
  });
