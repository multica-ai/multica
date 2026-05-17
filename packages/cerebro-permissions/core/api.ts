import { api, parseWithFallback } from "@multica/core/api";
import {
  grantAuditListSchema,
  personaGrantListSchema,
  personaGrantSchema,
  type CreatePersonaGrantRequest,
  type GrantAuditEntry,
  type GrantsFilter,
  type PaginatedResponse,
  type PersonaGrant,
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

export async function fetchGrantAudit(
  wsId: string,
  filter: {
    subjectId: string | null;
    grantId: string | null;
    since: string | null;
    limit: number;
    offset: number;
  },
): Promise<PaginatedResponse<GrantAuditEntry>> {
  const raw = await api.listPersonaGrantAudit<unknown>(wsId, {
    subject_id: filter.subjectId,
    grant_id: filter.grantId,
    since: filter.since,
    limit: filter.limit,
    offset: filter.offset,
  });
  return parseWithFallback(raw, grantAuditListSchema, EMPTY_AUDIT_PAGE, {
    endpoint: "listPersonaGrantAudit",
  });
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
