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

export async function fetchGrantAudit(
  wsId: string,
  filter: AuditFilter,
): Promise<PaginatedResponse<GrantAuditEntry>> {
  const raw = await api.listPersonaGrantAudit(wsId, {
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
// Subjects with permissions — derived from the grants list.
// ---------------------------------------------------------------------------

// There is no dedicated subjects endpoint yet — the grants list already
// carries everything the Subjects tab needs (subject, capability, resource,
// status, approval flag). We fetch the grants for the workspace and fold them
// into one row per subject. The grants endpoint returns the full set in a
// single response (no server-side pagination), so a single call is enough;
// filtering by subject type, search, and pagination all happen client-side.
export async function fetchSubjectsWithPermissions(
  wsId: string,
  filter: { subjectType: string | null; search: string; limit: number; offset: number },
): Promise<PaginatedResponse<SubjectWithPermissions>> {
  const grantsPage = await fetchPersonaGrants(wsId, {
    subjectType: (filter.subjectType as GrantsFilter["subjectType"]) ?? null,
    subjectId: null,
    resourceType: null,
    status: null,
    classification: null,
    search: "",
    // Backend returns every matching grant regardless of limit/offset; a high
    // limit guards against a future server that honours the param.
    limit: 1000,
    offset: 0,
  });

  const subjects = groupGrantsIntoSubjects(grantsPage.items);

  const q = filter.search.trim().toLowerCase();
  const filtered = q
    ? subjects.filter(
        (s) =>
          (s.display_name ?? "").toLowerCase().includes(q) ||
          s.id.toLowerCase().includes(q),
      )
    : subjects;

  const total = filtered.length;
  const offset = Math.max(0, filter.offset);
  const limit = filter.limit > 0 ? filter.limit : 50;
  const items = filtered.slice(offset, offset + limit);

  return { items, total, limit, offset };
}

// Fold a flat grant list into one SubjectWithPermissions per distinct subject.
// Subjects are keyed by type + id so a member and an agent that happen to share
// an id never collapse together; subjectless types (workspace_default,
// anonymous) key on their type alone.
function groupGrantsIntoSubjects(
  grants: PersonaGrant[],
): SubjectWithPermissions[] {
  const bySubject = new Map<string, SubjectWithPermissions>();

  for (const g of grants) {
    const subjectId = g.subject.id ?? "";
    const key = `${g.subject.type}:${subjectId}`;
    let entry = bySubject.get(key);
    if (!entry) {
      entry = {
        id: subjectId || g.subject.type,
        type: g.subject.type,
        display_name: g.subject.display_name ?? g.subject.id ?? null,
        avatar_url: g.subject.avatar_url ?? null,
        permissions: [],
        pending_count: 0,
      };
      bySubject.set(key, entry);
    }
    entry.permissions.push({
      capability: g.capability,
      resource_pattern: g.resource.pattern,
      status: g.status,
      approval_required: g.approval_required,
      grant_id: g.id,
    });
    if (g.status === "pending_approval") entry.pending_count += 1;
  }

  return Array.from(bySubject.values()).sort((a, b) =>
    (a.display_name ?? a.id).localeCompare(b.display_name ?? b.id),
  );
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
  const raw = await api.listPendingApprovalAsks(wsId, { limit: filter.limit, offset: filter.offset });
  const normalized = normalizeApprovalsList(raw);
  return parseWithFallback(normalized, pendingAsksPageSchema, EMPTY_ASKS_PAGE, {
    endpoint: "listPendingApprovalAsks",
  });
}

export async function approveAsk(wsId: string, askId: string): Promise<void> {
  await api.approveAsk(wsId, askId);
}

export async function rejectAsk(wsId: string, askId: string): Promise<void> {
  await api.rejectAsk(wsId, askId);
}

// Backend approval list returns { approvals: [...], ... }; frontend schema expects { items: [...], ... }.
// Each approval item maps requester_type/requester_id → subject, created_at → requested_at.
function normalizeApprovalsList(raw: unknown): unknown {
  if (!raw || typeof raw !== "object") return raw;
  const rec = raw as Record<string, unknown>;
  if (Array.isArray(rec.items)) return rec;
  const arr = Array.isArray(rec.approvals) ? rec.approvals : [];
  const items = arr.map((a: unknown) => {
    if (!a || typeof a !== "object") return a;
    const r = a as Record<string, unknown>;
    return {
      id: r.id,
      workspace_id: r.workspace_id,
      subject: { type: r.requester_type ?? "member", id: r.requester_id ?? null, display_name: null },
      capability: r.capability ?? "",
      resource: { type: "resource", pattern: typeof r.resource === "string" ? r.resource : "*" },
      reason: r.reason ?? null,
      requested_at: r.created_at ?? "",
      expires_at: r.expires_at ?? null,
      status: r.status ?? "pending",
    };
  });
  return {
    items,
    total: typeof rec.total === "number" ? rec.total : items.length,
    limit: typeof rec.limit === "number" ? rec.limit : 50,
    offset: typeof rec.offset === "number" ? rec.offset : 0,
  };
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
