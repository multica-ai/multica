import { z } from "zod";

// Proposed Persona grant shape (JEH-1180). Mirrors the contract described
// in JEH-1179 — subject × resource × capability × classification × time
// window × approval. The actual API is being built in parallel; until
// Fætta posts the final shape, every field is nullable / lenient so a
// drift between proposal and implementation doesn't white-screen the page.

export type GrantSubjectType =
  | "actor"
  | "agent"
  | "member"
  | "group"
  | "role"
  | "workspace_default"
  | "anonymous";

export type GrantStatus = "active" | "pending_approval" | "expired" | "revoked";

// Same DLP classification ladder Persona uses for data-classification ×
// actor decisions. UNCLASSIFIED → SECRET is the canonical order; "TOP"
// is reserved for future workspace-confidential rows.
export type GrantClassification =
  | "unclassified"
  | "internal"
  | "confidential"
  | "restricted"
  | "secret";

export const GRANT_CLASSIFICATIONS: GrantClassification[] = [
  "unclassified",
  "internal",
  "confidential",
  "restricted",
  "secret",
];

export interface GrantSubject {
  type: GrantSubjectType | string;
  id: string | null;
  display_name: string | null;
  avatar_url?: string | null;
}

export interface GrantResource {
  // High-level resource type (e.g. `issue`, `project`, `runtime`, `agent`,
  // `repo`, `mcp_server`, `secret`). Maps 1:1 to JEH-978's A/C/F tables.
  type: string;
  // Optional pattern within the type. `*` = all of type. Otherwise UUID or
  // glob like `repo:firtal-cerebro/*`.
  pattern: string;
  // Resolved display name when the pattern matches a single concrete row.
  display_name?: string | null;
}

export interface GrantTimeWindow {
  starts_at: string | null;
  ends_at: string | null;
}

export interface PersonaGrant {
  id: string;
  workspace_id: string;
  subject: GrantSubject;
  resource: GrantResource;
  // Capability name as Persona's capability register publishes it
  // (`issues.read`, `repo.push`, `secret.use`, …).
  capability: string;
  // Maximum data classification this grant may touch.
  classification_ceiling: GrantClassification | string;
  status: GrantStatus | string;
  approval_required: boolean;
  time_window: GrantTimeWindow | null;
  description: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreatePersonaGrantRequest {
  subject: GrantSubject;
  resource: GrantResource;
  capability: string;
  classification_ceiling: GrantClassification | string;
  approval_required: boolean;
  time_window?: GrantTimeWindow | null;
  description?: string | null;
}

export interface UpdatePersonaGrantRequest {
  capability?: string;
  resource?: GrantResource;
  classification_ceiling?: GrantClassification | string;
  approval_required?: boolean;
  time_window?: GrantTimeWindow | null;
  description?: string | null;
  status?: GrantStatus | string;
}

export interface GrantAuditEntry {
  id: string;
  workspace_id: string;
  grant_id: string | null;
  // `create` / `update` / `delete` / `approve` / `revoke`.
  action: string;
  actor_id: string | null;
  actor_name: string | null;
  actor_type: "member" | "agent" | string | null;
  // Surface that performed the change: ui / api / cli / mcp.
  via: "ui" | "api" | "cli" | "mcp" | string | null;
  // Free-form summary of what changed. Diff details live in `before`/`after`.
  summary: string | null;
  before: unknown;
  after: unknown;
  created_at: string;
}

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

// ---------------------------------------------------------------------------
// Lenient zod schemas — every non-key field nullable / defaulted so an
// older or newer backend cannot white-screen the page. See CLAUDE.md
// "API Response Compatibility".
// ---------------------------------------------------------------------------

const grantSubjectSchema: z.ZodType<GrantSubject> = z.object({
  type: z.string(),
  id: z.string().nullable().default(null),
  display_name: z.string().nullable().default(null),
  avatar_url: z.string().nullable().optional(),
});

const grantResourceSchema: z.ZodType<GrantResource> = z.object({
  type: z.string(),
  pattern: z.string().default("*"),
  display_name: z.string().nullable().optional(),
});

const grantTimeWindowSchema: z.ZodType<GrantTimeWindow> = z.object({
  starts_at: z.string().nullable().default(null),
  ends_at: z.string().nullable().default(null),
});

export const personaGrantSchema: z.ZodType<PersonaGrant> = z.object({
  id: z.string(),
  workspace_id: z.string(),
  subject: grantSubjectSchema,
  resource: grantResourceSchema,
  capability: z.string(),
  classification_ceiling: z.string().default("unclassified"),
  status: z.string().default("active"),
  approval_required: z.boolean().default(false),
  time_window: grantTimeWindowSchema.nullable().default(null),
  description: z.string().nullable().default(null),
  created_by: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
});

export const grantAuditEntrySchema: z.ZodType<GrantAuditEntry> = z.object({
  id: z.string(),
  workspace_id: z.string(),
  grant_id: z.string().nullable().default(null),
  action: z.string(),
  actor_id: z.string().nullable().default(null),
  actor_name: z.string().nullable().default(null),
  actor_type: z.string().nullable().default(null),
  via: z.string().nullable().default(null),
  summary: z.string().nullable().default(null),
  before: z.unknown().default(null),
  after: z.unknown().default(null),
  created_at: z.string(),
});

function paginatedSchema<T>(item: z.ZodType<T>): z.ZodType<PaginatedResponse<T>> {
  return z.object({
    items: z.array(item).default([]),
    total: z.number().default(0),
    limit: z.number().default(50),
    offset: z.number().default(0),
  });
}

export const personaGrantListSchema = paginatedSchema(personaGrantSchema);
export const grantAuditListSchema = paginatedSchema(grantAuditEntrySchema);

// ---------------------------------------------------------------------------
// Filter shape — owned by the Zustand store; passed through to the query
// key so refetches are automatic when the user changes a filter.
// ---------------------------------------------------------------------------

export interface GrantsFilter {
  subjectType: GrantSubjectType | null;
  subjectId: string | null;
  resourceType: string | null;
  status: GrantStatus | null;
  classification: GrantClassification | null;
  search: string;
  limit: number;
  offset: number;
}

export const DEFAULT_GRANTS_FILTER: GrantsFilter = {
  subjectType: null,
  subjectId: null,
  resourceType: null,
  status: null,
  classification: null,
  search: "",
  limit: 50,
  offset: 0,
};
