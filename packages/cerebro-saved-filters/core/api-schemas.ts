import { z } from "zod";

import type { FilterSnapshot, SavedFilter } from "./types";

export const EMPTY_SNAPSHOT: FilterSnapshot = {
  statusFilters: [],
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
  onBehalfOfFilters: [],
  agentRunningFilter: false,
};

const actorSchema = z.object({
  type: z.enum(["member", "agent", "squad"]),
  id: z.string(),
});

// Every field defaults so a partial / drifted snapshot from an older client
// still yields a usable filter rather than throwing into the dropdown.
const filterSnapshotSchema = z
  .object({
    statusFilters: z.array(z.string()).default([]),
    priorityFilters: z.array(z.string()).default([]),
    assigneeFilters: z.array(actorSchema).default([]),
    includeNoAssignee: z.boolean().default(false),
    creatorFilters: z.array(actorSchema).default([]),
    projectFilters: z.array(z.string()).default([]),
    includeNoProject: z.boolean().default(false),
    labelFilters: z.array(z.string()).default([]),
    onBehalfOfFilters: z.array(z.string()).default([]),
    agentRunningFilter: z.boolean().default(false),
  })
  .transform((s) => s as FilterSnapshot);

export const savedFilterSchema = z
  .object({
    id: z.string(),
    name: z.string(),
    surface: z.string().default("issues"),
    filter_state: filterSnapshotSchema.catch(EMPTY_SNAPSHOT),
    position: z.number().default(0),
    created_at: z.string().default(""),
    updated_at: z.string().default(""),
  })
  .transform(
    (row): SavedFilter => ({
      id: row.id,
      name: row.name,
      surface: row.surface,
      filterState: row.filter_state,
      position: row.position,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
    }),
  );

export const savedFilterListSchema = z.array(savedFilterSchema);
