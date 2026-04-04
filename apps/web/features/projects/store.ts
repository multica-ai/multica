"use client";

import { create } from "zustand";
import type { Project, CreateProjectRequest, UpdateProjectRequest } from "@/shared/types";
import { toast } from "sonner";
import { api } from "@/shared/api";

interface ProjectState {
  projects: Project[];
  loading: boolean;
  fetch: () => Promise<void>;
  addProject: (project: Project) => void;
  updateProject: (id: string, updates: Partial<Project>) => void;
  removeProject: (id: string) => void;
  createProject: (data: CreateProjectRequest) => Promise<Project>;
  updateProjectApi: (id: string, data: UpdateProjectRequest) => Promise<void>;
  deleteProject: (id: string) => Promise<void>;
}

export const useProjectStore = create<ProjectState>((set, get) => ({
  projects: [],
  loading: true,

  fetch: async () => {
    const isInitialLoad = get().projects.length === 0;
    if (isInitialLoad) set({ loading: true });
    try {
      const res = await api.listProjects();
      set({ projects: res.projects, loading: false });
    } catch {
      toast.error("Failed to load projects");
      if (isInitialLoad) set({ loading: false });
    }
  },

  addProject: (project) =>
    set((s) => ({
      projects: s.projects.some((p) => p.id === project.id)
        ? s.projects
        : [...s.projects, project],
    })),

  updateProject: (id, updates) =>
    set((s) => ({
      projects: s.projects.map((p) => (p.id === id ? { ...p, ...updates } : p)),
    })),

  removeProject: (id) =>
    set((s) => ({ projects: s.projects.filter((p) => p.id !== id) })),

  createProject: async (data) => {
    const project = await api.createProject(data);
    get().addProject(project);
    return project;
  },

  updateProjectApi: async (id, data) => {
    const prev = get().projects.find((p) => p.id === id);
    get().updateProject(id, data);
    try {
      const updated = await api.updateProject(id, data);
      get().updateProject(id, updated);
    } catch {
      if (prev) get().updateProject(id, prev);
      toast.error("Failed to update project");
    }
  },

  deleteProject: async (id) => {
    const prev = get().projects;
    get().removeProject(id);
    try {
      await api.deleteProject(id);
      toast.success("Project deleted");
    } catch {
      set({ projects: prev });
      toast.error("Failed to delete project");
    }
  },
}));
