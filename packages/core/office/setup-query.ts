import { queryOptions } from "@tanstack/react-query";
import { AGENT_INSTRUCTION_TEMPLATES } from "../agents/instruction-templates";
import { api } from "../api";
import type { OfficeSetupRequest } from "./setup-schema";

export const officeSetupKeys = {
  all: (wsId: string) => ["office-setup", wsId] as const,
};

/**
 * The Secretary's readiness checklist. It changes only when the boss runs setup
 * or a setup-related row changes, so it stays fresh for a short time; the
 * caller refetches after a successful setup.
 */
export function officeSetupOptions(wsId: string) {
  return queryOptions({
    queryKey: officeSetupKeys.all(wsId),
    queryFn: () => api.getOfficeSetup(),
    staleTime: 30_000,
  });
}

/**
 * The body of `POST /api/office/setup`: the secretary instruction template from
 * the agent templates, and the timezone of the 08:00 brief. The timezone
 * defaults to the browser's; when the runtime cannot tell, it is left out and
 * the server falls back to the caller's saved timezone, then UTC.
 */
export function buildOfficeSetupRequest(timezone?: string): OfficeSetupRequest {
  const template = AGENT_INSTRUCTION_TEMPLATES.find((t) => t.id === "secretary");
  if (!template) throw new Error('no instruction template "secretary"');
  const zone = timezone ?? browserTimezone();
  return {
    secretary_instructions: template.instructions,
    ...(zone ? { timezone: zone } : {}),
  };
}

function browserTimezone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
  } catch {
    return undefined;
  }
}
