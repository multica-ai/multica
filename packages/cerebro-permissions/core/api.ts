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
  return parseWithFallback(raw, personaGrantListSchema, EMPTY_GRANT_PAGE, {
    endpoint: "listPersonaGrants",
  });
}

export async function fetchPersonaGrant(
  wsId: string,
  grantId: string,
): Promise<PersonaGrant | null> {
  const raw = await api.getPersonaGrant<unknown>(wsId, grantId);
  return parseWithFallback(raw, personaGrantSchema, null, {
    endpoint: "getPersonaGrant",
  });
}

export async function createPersonaGrant(
  wsId: string,
  body: CreatePersonaGrantRequest,
): Promise<PersonaGrant | null> {
  const raw = await api.createPersonaGrant<unknown>(wsId, body);
  return parseWithFallback(raw, personaGrantSchema, null, {
    endpoint: "createPersonaGrant",
  });
}

export async function updatePersonaGrant(
  wsId: string,
  grantId: string,
  body: UpdatePersonaGrantRequest,
): Promise<PersonaGrant | null> {
  const raw = await api.updatePersonaGrant<unknown>(wsId, grantId, body);
  return parseWithFallback(raw, personaGrantSchema, null, {
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
