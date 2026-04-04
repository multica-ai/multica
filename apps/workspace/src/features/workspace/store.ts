"use client";

import { create } from "zustand";
import type { Workspace, MemberWithUser, Agent, Skill } from "@/shared/types";
import { useIssueStore } from "@/features/issues";
import { useInboxStore } from "@/features/inbox";
import { useRuntimeStore } from "@/features/runtimes";
import { toast } from "sonner";
import { api } from "@/shared/api";
import { createLogger } from "@/shared/logger";

const logger = createLogger("workspace-store");

interface WorkspaceState {
  workspace: Workspace | null;
  workspaces: Workspace[];
  members: MemberWithUser[];
  agents: Agent[];
  skills: Skill[];
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

type WorkspaceStore = WorkspaceState & WorkspaceActions;

export const useWorkspaceStore = create<WorkspaceStore>((set, get) => ({
  // State
  workspace: null,
  workspaces: [],
  members: [],
  agents: [],
  skills: [],

  // Actions
  hydrateWorkspace: async (wsList, preferredWorkspaceId) => {
    set({ workspaces: wsList });

    const nextWorkspace =
      (preferredWorkspaceId
        ? wsList.find((item) => item.id === preferredWorkspaceId)
        : null) ??
      wsList[0] ??
      null;

    if (!nextWorkspace) {
      api.setWorkspaceId(null);
      localStorage.removeItem("multica_workspace_id");
      set({ workspace: null, members: [], agents: [], skills: [] });
      return null;
    }

    api.setWorkspaceId(nextWorkspace.id);
    localStorage.setItem("multica_workspace_id", nextWorkspace.id);
    set({ workspace: nextWorkspace });

    logger.debug("hydrate workspace", nextWorkspace.name, nextWorkspace.id);
    const [nextMembers, nextAgents, nextSkills] = await Promise.all([
      api.listMembers(nextWorkspace.id).catch((e) => {
        logger.error("failed to load members", e);
        toast.error("Failed to load members");
        return [] as MemberWithUser[];
      }),
      api.listAgents({ workspace_id: nextWorkspace.id, include_archived: true }).catch((e) => {
        logger.error("failed to load agents", e);
        toast.error("Failed to load agents");
        return [] as Agent[];
      }),
      api.listSkills().catch(() => [] as Skill[]),
      useIssueStore.getState().fetch().catch(() => {}),
      useInboxStore.getState().fetch().catch(() => {}),
    ]);
    logger.info("hydrate complete", "members:", nextMembers.length, "agents:", nextAgents.length);
    set({ members: nextMembers, agents: nextAgents, skills: nextSkills });

    return nextWorkspace;
  },

  switchWorkspace: async (workspaceId) => {
    logger.info("switching to", workspaceId);
    const { workspaces, hydrateWorkspace } = get();
    const ws = workspaces.find((item) => item.id === workspaceId);
    if (!ws) return;

    // Switch identity FIRST — api client, localStorage, and the
    // workspace object in this store — so that any in-flight refetch
    // (e.g. triggered by a WS event during the async gap) already
    // targets the new workspace.
    api.setWorkspaceId(ws.id);
    localStorage.setItem("multica_workspace_id", ws.id);

    // Clear ALL stale data across every store before hydrating.
    useIssueStore.getState().setIssues([]);
    useInboxStore.getState().setItems([]);
    useRuntimeStore.getState().setRuntimes([]);
    set({ workspace: ws, members: [], agents: [], skills: [] });

    await hydrateWorkspace(workspaces, ws.id);
  },

  refreshWorkspaces: async () => {
    const { workspace, hydrateWorkspace } = get();
    const storedWorkspaceId = localStorage.getItem("multica_workspace_id");
    try {
      const wsList = await api.listWorkspaces();
      await hydrateWorkspace(wsList, workspace?.id ?? storedWorkspaceId);
      return wsList;
    } catch (e) {
      logger.error("failed to refresh workspaces", e);
      toast.error("Failed to refresh workspaces");
      return get().workspaces;
    }
  },

  refreshMembers: async () => {
    const { workspace } = get();
    if (!workspace) return;
    try {
      const members = await api.listMembers(workspace.id);
      set({ members });
    } catch (e) {
      logger.error("failed to refresh members", e);
      toast.error("Failed to refresh members");
    }
  },

  updateAgent: (id, updates) =>
    set((s) => ({
      agents: s.agents.map((a) => (a.id === id ? { ...a, ...updates } : a)),
    })),

  refreshAgents: async () => {
    const { workspace } = get();
    if (!workspace) return;
    try {
      const agents = await api.listAgents({ workspace_id: workspace.id, include_archived: true });
      set({ agents });
    } catch (e) {
      logger.error("failed to refresh agents", e);
      toast.error("Failed to refresh agents");
    }
  },

  refreshSkills: async () => {
    const { workspace, skills: existing } = get();
    if (!workspace) return;
    try {
      const fetched = await api.listSkills();
      // listSkills doesn't include files — preserve files from existing entries
      const filesById = new Map(
        existing.filter((s) => s.files?.length).map((s) => [s.id, s.files]),
      );
      const merged = fetched.map((s) => ({
        ...s,
        files: s.files ?? filesById.get(s.id) ?? [],
      }));
      set({ skills: merged });
    } catch (e) {
      logger.error("failed to refresh skills", e);
      toast.error("Failed to refresh skills");
    }
  },

  upsertSkill: (skill) => {
    set((state) => {
      const idx = state.skills.findIndex((s) => s.id === skill.id);
      if (idx >= 0) {
        const next = [...state.skills];
        next[idx] = skill;
        return { skills: next };
      }
      return { skills: [...state.skills, skill] };
    });
  },

  removeSkill: (id) => {
    set((state) => ({ skills: state.skills.filter((s) => s.id !== id) }));
  },

  createWorkspace: async (data) => {
    const ws = await api.createWorkspace(data);
    set((state) => ({ workspaces: [...state.workspaces, ws] }));
    return ws;
  },

  updateWorkspace: (ws) => {
    set((state) => ({
      workspace: state.workspace?.id === ws.id ? ws : state.workspace,
      workspaces: state.workspaces.map((item) =>
        item.id === ws.id ? ws : item,
      ),
    }));
  },

  leaveWorkspace: async (workspaceId) => {
    await api.leaveWorkspace(workspaceId);
    const { workspace, hydrateWorkspace } = get();
    const wsList = await api.listWorkspaces();
    const preferredWorkspaceId =
      workspace?.id === workspaceId ? null : (workspace?.id ?? null);
    await hydrateWorkspace(wsList, preferredWorkspaceId);
  },

  deleteWorkspace: async (workspaceId) => {
    await api.deleteWorkspace(workspaceId);
    const { workspace, hydrateWorkspace } = get();
    const wsList = await api.listWorkspaces();
    const preferredWorkspaceId =
      workspace?.id === workspaceId ? null : (workspace?.id ?? null);
    await hydrateWorkspace(wsList, preferredWorkspaceId);
  },

  clearWorkspace: () => {
    api.setWorkspaceId(null);
    localStorage.removeItem("multica_workspace_id");
    set({ workspace: null, workspaces: [], members: [], agents: [], skills: [] });
  },
}));
