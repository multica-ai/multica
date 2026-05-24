import { api, parseWithFallback } from "@multica/core/api";
import { z } from "zod";
import {
  grantAuditListSchema,
  personaGrantListSchema,
  personaGrantSchema,
  type AuditFilter,
  type CreatePersonaGrantRequest,
  type EffectivePermissionRequest,
  type EffectivePermissionResult,
  type GrantAuditEntry,
  type GrantsFilter,
  type PaginatedResponse,
  type PendingAsk,
  type PersonaGrant,
  type SubjectWithPermissions,
  type UpdatePersonaGrantRequest,
} from "./types";

const EMPTY_GRANT_PAGE: PaginatedResponse<PersonaGrant> = {
  items: [],
  total: 0,
  limit: 50,
  offset: 0,
};

const EMPTY_AUDIT_PAGE: PaginatedResponse<GrantAuditEntry> = {
  items: [],
  total: 0,
  limit: 50,
  offset: 0,
};

export async function fetchPersonaGrants(
  wsId: string,
  filter: GrantsFilter,
): Promise<PaginatedResponse<PersonaGrant>> {
  const raw = await api.listPersonaGrants<unknown>(wsId, {
    subject_type: filter.subjectType,
    subject_id: filter.subjectId,
    resource_type: filter.resourceType,
    status: filter.status,
    classification: filter.classification,
    limit: filter.limit,
    offset: filter.offset,
  });
  return parseWithFallback(normalizeGrantList(raw), personaGrantListSchema, EMPTY_GRANT_PAGE, {
    endpoint: "listPersonaGrants",
  });
}

export async function fetchPersonaGrant(
  wsId: string,
  grantId: string,
): Promise<PersonaGrant | null> {
  const raw = await api.getPersonaGrant<unknown>(wsId, grantId);
  return parseWithFallback(normalizeGrant(raw), personaGrantSchema, null, {
    endpoint: "getPersonaGrant",
  });
}

export async function createPersonaGrant(
  wsId: string,
  body: CreatePersonaGrantRequest,
): Promise<PersonaGrant | null> {
  const raw = await api.createPersonaGrant<unknown>(wsId, toBackendCreate(body));
  return parseWithFallback(normalizeGrant(raw), personaGrantSchema, null, {
    endpoint: "createPersonaGrant",
  });
}

export async function updatePersonaGrant(
  wsId: string,
  grantId: string,
  body: UpdatePersonaGrantRequest,
): Promise<PersonaGrant | null> {
  const raw = await api.updatePersonaGrant<unknown>(wsId, grantId, toBackendUpdate(body));
  return parseWithFallback(normalizeGrant(raw), personaGrantSchema, null, {
    endpoint: "updatePersonaGrant",
  });
}

export async function deletePersonaGrant(
  wsId: string,
  grantId: string,
): Promise<void> {
  await api.deletePersonaGrant(wsId, grantId);
}

export async function evaluateEffectivePermission(
  wsId: string,
  body: EffectivePermissionRequest,
): Promise<EffectivePermissionResult> {
  const raw = await api.evaluatePersonaGrant<unknown>(wsId, body);
  const rec = raw && typeof raw === "object" ? (raw as Record<string, unknown>) : {};
  return {
    decision: typeof rec.decision === "string" ? rec.decision : "deny",
    reason: typeof rec.reason === "string" ? rec.reason : "no result",
    matched_grant_ids: Array.isArray(rec.matched_grant_ids)
      ? rec.matched_grant_ids.filter((id): id is string => typeof id === "string")
      : [],
    winning_override_layer:
      typeof rec.winning_override_layer === "string"
        ? rec.winning_override_layer
        : "",
  };
}

// CEREBRO-PATCH(audit-extended-filter): capability + until params added by FIR-2133; cast needed until core client type is updated upstream.
type AuditApiFilter = { subject_id?: string | null; grant_id?: string | null; capability?: string | null; since?: string | null; until?: string | null; limit?: number; offset?: number };
const _auditApi = api.listPersonaGrantAudit as (wsId: string, f: AuditApiFilter) => Promise<unknown>;

export async function fetchGrantAudit(
  wsId: string,
  filter: AuditFilter,
): Promise<PaginatedResponse<GrantAuditEntry>> {
  const raw = await _auditApi(wsId, {
    subject_id: filter.subjectId,
    grant_id: filter.grantId,
    capability: filter.capability,
    since: filter.since,
    until: filter.until,
    limit: filter.limit,
    offset: filter.offset,
  });
  return parseWithFallback(raw, grantAuditListSchema, EMPTY_AUDIT_PAGE, {
    endpoint: "listPersonaGrantAudit",
  });
}

// ---------------------------------------------------------------------------
// Subjects with permissions — Phase 2 API (mock-ready, stubs until live).
// ---------------------------------------------------------------------------

const EMPTY_SUBJECTS_PAGE: PaginatedResponse<SubjectWithPermissions> = {
  items: [],
  total: 0,
  limit: 50,
  offset: 0,
};

const subjectPermissionSchema = z.object({
  capability: z.string(),
  resource_pattern: z.string().default("*"),
  status: z.string().default("active"),
  approval_required: z.boolean().default(false),
  grant_id: z.string(),
});

const subjectWithPermissionsSchema: z.ZodType<SubjectWithPermissions> = z.object({
  id: z.string(),
  type: z.string(),
  display_name: z.string().nullable().default(null),
  avatar_url: z.string().nullable().optional(),
  permissions: z.array(subjectPermissionSchema).default([]),
  pending_count: z.number().default(0),
});

const subjectsPageSchema = z.object({
  items: z.array(subjectWithPermissionsSchema).default([]),
  total: z.number().default(0),
  limit: z.number().default(50),
  offset: z.number().default(0),
});

export async function fetchSubjectsWithPermissions(
  wsId: string,
  filter: { subjectType: string | null; search: string; limit: number; offset: number },
): Promise<PaginatedResponse<SubjectWithPermissions>> {
  // CEREBRO-PATCH(access-page-subjects-stub): Phase 2 API not yet deployed — return empty page.
  // Remove stub and wire `api.listSubjectsWithPermissions` when Phase 2 lands.
  try {
    const raw = await (api as unknown as Record<string, (wsId: string, p: unknown) => Promise<unknown>>)
      .listSubjectsWithPermissions?.(wsId, {
        subject_type: filter.subjectType,
        search: filter.search,
        limit: filter.limit,
        offset: filter.offset,
      });
    if (!raw) return EMPTY_SUBJECTS_PAGE;
    return parseWithFallback(raw, subjectsPageSchema, EMPTY_SUBJECTS_PAGE, {
      endpoint: "listSubjectsWithPermissions",
    });
  } catch {
    return EMPTY_SUBJECTS_PAGE;
  }
}

// ---------------------------------------------------------------------------
// Pending asks — Phase 3 API (mock-ready, stubs until live).
// ---------------------------------------------------------------------------

const EMPTY_ASKS_PAGE: PaginatedResponse<PendingAsk> = {
  items: [],
  total: 0,
  limit: 50,
  offset: 0,
};

const pendingAskSchema: z.ZodType<PendingAsk> = z.object({
  id: z.string(),
  workspace_id: z.string(),
  subject: z.object({
    type: z.string(),
    id: z.string().nullable().default(null),
    display_name: z.string().nullable().default(null),
  }),
  capability: z.string(),
  resource: z.object({
    type: z.string(),
    pattern: z.string().default("*"),
    display_name: z.string().nullable().optional(),
  }),
  reason: z.string().nullable().default(null),
  requested_at: z.string(),
  expires_at: z.string().nullable().default(null),
  status: z.string().default("pending"),
});

const pendingAsksPageSchema = z.object({
  items: z.array(pendingAskSchema).default([]),
  total: z.number().default(0),
  limit: z.number().default(50),
  offset: z.number().default(0),
});

export async function fetchPendingAsks(
  wsId: string,
  filter: { limit: number; offset: number },
): Promise<PaginatedResponse<PendingAsk>> {
  // CEREBRO-PATCH(access-page-pending-stub): Phase 3 API not yet deployed — return empty page.
  // Remove stub and wire `api.listPendingApprovalAsks` when Phase 3 lands.
  try {
    const raw = await (api as unknown as Record<string, (wsId: string, p: unknown) => Promise<unknown>>)
      .listPendingApprovalAsks?.(wsId, { limit: filter.limit, offset: filter.offset });
    if (!raw) return EMPTY_ASKS_PAGE;
    return parseWithFallback(raw, pendingAsksPageSchema, EMPTY_ASKS_PAGE, {
      endpoint: "listPendingApprovalAsks",
    });
  } catch {
    return EMPTY_ASKS_PAGE;
  }
}

export async function approveAsk(wsId: string, askId: string): Promise<void> {
  await (api as unknown as Record<string, (wsId: string, askId: string) => Promise<void>>)
    .approveAsk?.(wsId, askId);
}

export async function rejectAsk(wsId: string, askId: string): Promise<void> {
  await (api as unknown as Record<string, (wsId: string, askId: string) => Promise<void>>)
    .rejectAsk?.(wsId, askId);
}

function normalizeGrantList(raw: unknown): unknown {
  if (!raw || typeof raw !== "object") return raw;
  const rec = raw as Record<string, unknown>;
  if (Array.isArray(rec.items)) return rec;
  if (!Array.isArray(rec.grants)) return raw;
  const items = rec.grants.map(normalizeGrant);
  return {
    items,
    total: typeof rec.total === "number" ? rec.total : items.length,
    limit: typeof rec.limit === "number" ? rec.limit : 50,
    offset: typeof rec.offset === "number" ? rec.offset : 0,
  };
}

function normalizeGrant(raw: unknown): unknown {
  if (!raw || typeof raw !== "object") return raw;
  const rec = raw as Record<string, unknown>;
  if (rec.subject && rec.resource) return raw;
  return {
    id: rec.id,
    workspace_id: rec.workspace_id,
    subject: {
      type: rec.subject_type ?? "workspace_default",
      id: rec.subject_id ?? null,
      display_name: rec.subject_id ?? null,
    },
    resource: {
      type: inferResourceType(String(rec.resource_pattern ?? "*")),
      pattern: rec.resource_pattern ?? "*",
    },
    capability: rec.capability,
    classification_ceiling: rec.classification_ceiling ?? "unclassified",
    status: rec.status ?? "active",
    approval_required: Boolean(rec.approval_required),
    time_window:
      rec.time_window_start || rec.time_window_end
        ? {
            starts_at: rec.time_window_start ?? null,
            ends_at: rec.time_window_end ?? null,
          }
        : null,
    description: null,
    created_by: rec.granted_by_id ?? null,
    created_at: rec.granted_at ?? rec.created_at,
    updated_at: rec.updated_at ?? rec.granted_at ?? rec.created_at,
  };
}

function toBackendCreate(body: CreatePersonaGrantRequest) {
  return {
    subject_type: body.subject.type,
    subject_id: body.subject.id,
    resource_pattern: body.resource.pattern,
    capability: body.capability,
    classification_ceiling: body.classification_ceiling,
    time_window_start: body.time_window?.starts_at ?? null,
    time_window_end: body.time_window?.ends_at ?? null,
    approval_required: body.approval_required,
  };
}

function toBackendUpdate(body: UpdatePersonaGrantRequest) {
  return {
    resource_pattern: body.resource?.pattern,
    capability: body.capability,
    classification_ceiling: body.classification_ceiling,
    time_window_start: body.time_window?.starts_at ?? null,
    time_window_end: body.time_window?.ends_at ?? null,
    approval_required: body.approval_required,
    status: body.status,
  };
}

function inferResourceType(pattern: string): string {
  const [prefix] = pattern.split(/[/:]/, 1);
  return prefix && prefix !== "*" ? prefix : "workspace";
}
