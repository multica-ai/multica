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
  // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — saved date conditions.
  dateFilters: [],
};

const actorSchema = z.object({
  type: z.enum(["member", "agent", "squad"]),
  id: z.string(),
});

// CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — a single stacked date
// condition, captured in a saved filter preset.
const dateFilterSchema = z.object({
  field: z.enum(["created_at", "updated_at", "due_date"]),
  from: z.string(),
  to: z.string(),
  mode: z.enum(["range", "none"]).optional(),
  preset: z
    .enum(["today", "last_3_days", "last_7_days", "overdue", "this_week", "none", "custom"])
    .optional(),
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
    // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — stacked date conditions.
    dateFilters: z.array(dateFilterSchema).default([]),
  })
  .transform((s) => s as FilterSnapshot);

// Visibility drift downgrades to "private" rather than throwing — a never-seen
// value from a newer backend must not white-screen the dropdown.
const visibilitySchema = z
  .enum(["private", "shared", "workspace"])
  .catch("private");

const shareTargetSchema = z
  .object({
    target_type: z.enum(["group", "member"]),
    target_id: z.string(),
  })
  .transform((s) => ({ targetType: s.target_type, targetId: s.target_id }));

export const savedFilterSchema = z
  .object({
    id: z.string(),
    name: z.string(),
    surface: z.string().default("issues"),
    filter_state: filterSnapshotSchema.catch(EMPTY_SNAPSHOT),
    position: z.number().default(0),
    visibility: visibilitySchema.default("private"),
    owner_id: z.string().default(""),
    owner_name: z.string().default(""),
    is_owner: z.boolean().default(true),
    shares: z.array(shareTargetSchema).optional(),
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
      visibility: row.visibility,
      ownerId: row.owner_id,
      ownerName: row.owner_name || undefined,
      isOwner: row.is_owner,
      shares: row.shares,
      createdAt: row.created_at,
      updatedAt: row.updated_at,
    }),
  );

export const savedFilterListSchema = z.array(savedFilterSchema);

// The can-share gate response. Defaults to false so a drifted/empty body never
// accidentally exposes the share controls.
export const canShareSchema = z
  .object({ can_share: z.boolean().default(false) })
  .transform((r) => r.can_share)
  .catch(false);
