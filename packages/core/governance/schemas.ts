import { z } from "zod";
import {
  GOVERNANCE_ACTOR_TYPES,
  GOVERNANCE_EVENT_KINDS,
  type CompanyState,
  type CompanyStateInfo,
  type GovernanceActorType,
  type GovernanceEvent,
  type GovernanceEventKind,
  type GovernanceEventPage,
  type QuietHours,
} from "./types";

// Lenient on purpose (see parseWithFallback): missing fields take defaults,
// unknown fields pass through, and an enum this client does not know degrades
// to `unknown` instead of failing the whole log or the company state.

const KNOWN_COMPANY_STATES: readonly CompanyState[] = ["running", "paused", "quiet"];

const companyStateSchema = z
  .string()
  .default("unknown")
  .transform((state): CompanyState =>
    (KNOWN_COMPANY_STATES as readonly string[]).includes(state) ? (state as CompanyState) : "unknown",
  );

const eventKindSchema = z
  .string()
  .default("unknown")
  .transform((kind): GovernanceEventKind =>
    (GOVERNANCE_EVENT_KINDS as readonly string[]).includes(kind) ? (kind as GovernanceEventKind) : "unknown",
  );

const actorTypeSchema = z
  .string()
  .default("unknown")
  .transform((type): GovernanceActorType =>
    (GOVERNANCE_ACTOR_TYPES as readonly string[]).includes(type) ? (type as GovernanceActorType) : "unknown",
  );

const optionalId = z
  .string()
  .nullish()
  .transform((id) => id || null);

export const QuietHoursSchema = z
  .object({
    enabled: z.boolean().default(false),
    start: z.string().default("22:00"),
    end: z.string().default("07:00"),
    timezone: z.string().default("UTC"),
  })
  .loose()
  .transform((hours): QuietHours => hours);

/** A setting is what the boss chose. A server that omits it has no quiet window to differ from `state`. */
function settingOf(value: string | null | undefined, state: CompanyState): CompanyState {
  if (value == null) return state;
  return (KNOWN_COMPANY_STATES as readonly string[]).includes(value) ? (value as CompanyState) : "unknown";
}

export const CompanyStateSchema = z
  .object({
    state: companyStateSchema,
    setting: z.string().nullish(),
    quiet_hours: QuietHoursSchema.default({ enabled: false, start: "22:00", end: "07:00", timezone: "UTC" }),
    updated_at: z.string().default(""),
  })
  .loose()
  .transform((info): CompanyStateInfo => ({
    state: info.state,
    setting: settingOf(info.setting, info.state),
    quiet_hours: info.quiet_hours,
    updated_at: info.updated_at,
  }));

/** What a company state response degrades to: a company whose state is not known. */
export const FALLBACK_COMPANY_STATE: CompanyStateInfo = {
  state: "unknown",
  setting: "unknown",
  quiet_hours: { enabled: false, start: "22:00", end: "07:00", timezone: "UTC" },
  updated_at: "",
};

export const GovernanceEventSchema = z
  .object({
    id: z.string(),
    kind: eventKindSchema,
    actor_type: actorTypeSchema,
    actor_id: optionalId,
    subject_type: z.string().default(""),
    subject_id: optionalId,
    summary: z.string().default(""),
    // A non-object (or null) details value is dropped to {}, not a failed event.
    details: z
      .record(z.string(), z.unknown())
      .catch({})
      .transform((details): Record<string, unknown> => details),
    created_at: z.string().default(""),
  })
  .loose()
  .transform((event): GovernanceEvent => event);

/** `GET /api/governance/events`: one page of the log, newest first, with the cursor for the next. */
export const GovernanceEventPageSchema = z
  .object({
    events: z.array(GovernanceEventSchema).default([]),
    next_cursor: z
      .string()
      .nullish()
      .transform((cursor) => cursor || null),
  })
  .loose()
  .transform((page): GovernanceEventPage => page);

export const EMPTY_GOVERNANCE_EVENT_PAGE: GovernanceEventPage = { events: [], next_cursor: null };
