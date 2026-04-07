"use client";

import { create } from "zustand";
import { api } from "@/shared/api";
import type { Agentflow, AgentflowTrigger, AgentflowRun } from "@/shared/types";

interface AgentflowState {
  agentflows: Agentflow[];
  loading: boolean;
  activeAgentflowId: string | null;
  triggers: Record<string, AgentflowTrigger[]>;
  runs: Record<string, AgentflowRun[]>;

  fetch: () => Promise<void>;
  setActiveAgentflow: (id: string | null) => void;
  addAgentflow: (af: Agentflow) => void;
  updateAgentflow: (id: string, updates: Partial<Agentflow>) => void;
  removeAgentflow: (id: string) => void;

  fetchTriggers: (agentflowId: string) => Promise<void>;
  fetchRuns: (agentflowId: string) => Promise<void>;
}

export const useAgentflowStore = create<AgentflowState>((set, get) => ({
  agentflows: [],
  loading: false,
  activeAgentflowId: null,
  triggers: {},
  runs: {},

  fetch: async () => {
    set({ loading: true });
    try {
      const agentflows = await api.listAgentflows();
      set({ agentflows, loading: false });
    } catch {
      set({ loading: false });
    }
  },

  setActiveAgentflow: (id) => set({ activeAgentflowId: id }),

  addAgentflow: (af) =>
    set((state) => ({ agentflows: [af, ...state.agentflows] })),

  updateAgentflow: (id, updates) =>
    set((state) => ({
      agentflows: state.agentflows.map((af) =>
        af.id === id ? { ...af, ...updates } : af
      ),
    })),

  removeAgentflow: (id) =>
    set((state) => ({
      agentflows: state.agentflows.filter((af) => af.id !== id),
    })),

  fetchTriggers: async (agentflowId) => {
    try {
      const triggers = await api.listAgentflowTriggers(agentflowId);
      set((state) => ({
        triggers: { ...state.triggers, [agentflowId]: triggers },
      }));
    } catch {
      // ignore
    }
  },

  fetchRuns: async (agentflowId) => {
    try {
      const runs = await api.listAgentflowRuns(agentflowId, { limit: 50 });
      set((state) => ({
        runs: { ...state.runs, [agentflowId]: runs },
      }));
    } catch {
      // ignore
    }
  },
}));
