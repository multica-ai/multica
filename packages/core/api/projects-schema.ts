import { z } from "zod";
import type { ListProjectUpdatesResponse, ProjectUpdate } from "../types/project";

const ProjectHealthSchema = z.enum(["on_track", "at_risk", "off_track"]);

export const ProjectUpdateSchema = z.object({
  id: z.string(),
  project_id: z.string(),
  workspace_id: z.string(),
  health: ProjectHealthSchema,
  body: z.string().default(""),
  author_type: z.enum(["member", "agent"]),
  author_id: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const ListProjectUpdatesResponseSchema = z.object({
  updates: z.array(ProjectUpdateSchema).default([]),
  total: z.number().default(0),
});

export const EMPTY_PROJECT_UPDATES: ListProjectUpdatesResponse = {
  updates: [],
  total: 0,
};

export type ParsedProjectUpdate = ProjectUpdate;
