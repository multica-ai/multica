"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createSafeId } from "../../utils";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export interface AgentBulkEditPresetEnvOperation {
  action: "set" | "remove";
  key: string;
}

export interface AgentBulkEditPresetCustomArgOperation {
  action: "add" | "replace" | "remove";
  value: string;
  replacement?: string;
}

export interface AgentBulkEditPresetPatch {
  runtimeId?: string;
  model?: string;
  maxConcurrentTasks?: number;
  customArgsPatch?: AgentBulkEditPresetCustomArgOperation[];
  env?: AgentBulkEditPresetEnvOperation[];
}

export interface AgentBulkEditPreset {
  id: string;
  name: string;
  patch: AgentBulkEditPresetPatch;
  updatedAt: number;
}

interface AgentBulkEditPresetsState {
  presets: AgentBulkEditPreset[];
  savePreset: (name: string, patch: AgentBulkEditPresetPatch) => string;
  removePreset: (id: string) => void;
}

const MAX_PRESETS = 12;

export const useAgentBulkEditPresetsStore = create<AgentBulkEditPresetsState>()(
  persist(
    (set) => ({
      presets: [],
      savePreset: (name, patch) => {
        const id = createSafeId();
        const preset: AgentBulkEditPreset = {
          id,
          name: name.trim(),
          patch: sanitizePresetPatch(patch),
          updatedAt: Date.now(),
        };
        set((state) => ({
          presets: [
            preset,
            ...state.presets.filter((existing) => existing.name !== preset.name),
          ].slice(0, MAX_PRESETS),
        }));
        return id;
      },
      removePreset: (id) =>
        set((state) => ({
          presets: state.presets.filter((preset) => preset.id !== id),
        })),
    }),
    {
      name: "multica_agent_bulk_edit_presets",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ presets: state.presets }),
      merge: (persisted, current) => {
        const raw = (persisted as Partial<AgentBulkEditPresetsState> | undefined)?.presets;
        if (!raw) return { ...current, presets: [] };
        return {
          ...current,
          presets: raw
            .map((preset) => ({
              ...preset,
              patch: sanitizePresetPatch(preset.patch),
            }))
            .slice(0, MAX_PRESETS),
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useAgentBulkEditPresetsStore.persist.rehydrate());

function sanitizePresetPatch(patch: AgentBulkEditPresetPatch): AgentBulkEditPresetPatch {
  const out: AgentBulkEditPresetPatch = {};

  if (patch.runtimeId !== undefined) out.runtimeId = patch.runtimeId;
  if (patch.model !== undefined) out.model = patch.model;
  if (patch.maxConcurrentTasks !== undefined) {
    out.maxConcurrentTasks = patch.maxConcurrentTasks;
  }
  if (patch.customArgsPatch !== undefined) {
    const customArgsPatch = patch.customArgsPatch
      .map((op) => ({
        action: normalizeCustomArgAction(op.action),
        value: op.value.trim(),
        replacement: op.replacement?.trim(),
      }))
      .filter((op) =>
        op.value.length > 0 &&
        (op.action !== "replace" || (op.replacement?.length ?? 0) > 0),
      );
    if (customArgsPatch.length > 0) out.customArgsPatch = customArgsPatch;
  }
  if (patch.env !== undefined) {
    const env = patch.env
      .map((op) => ({
        action: op.action === "remove" ? "remove" as const : "set" as const,
        key: op.key.trim(),
      }))
      .filter((op) => op.key.length > 0);
    if (env.length > 0) out.env = env;
  }

  return out;
}

function normalizeCustomArgAction(
  action: AgentBulkEditPresetCustomArgOperation["action"],
): AgentBulkEditPresetCustomArgOperation["action"] {
  if (action === "replace" || action === "remove") return action;
  return "add";
}
