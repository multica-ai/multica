import { z } from "zod";

/**
 * The Secretary setup (`GET` and `POST /api/office/setup`). The readiness
 * checklist drives the first-run card. Every field defaults, so an older or
 * newer server never blanks it, and an unreadable body reads as "not set up"
 * rather than as ready.
 */

export interface OfficeSetupReadiness {
  /** An online Hermes runtime the caller may use is in the workspace. */
  runtime_online: boolean;
  /** The workspace's live Secretary agent, or null when none is named. */
  secretary_id: string | null;
  /** A floor named Executive exists. */
  executive_floor: boolean;
  /** The Secretary's daily brief autopilot exists and is active. */
  autopilot_daily: boolean;
  /** The Secretary's weekly review autopilot exists and is paused. */
  autopilot_weekly_paused: boolean;
  /** The server's own verdict. Use isOfficeSetupReady for the decision. */
  ready: boolean;
}

export interface OfficeSetupRequest {
  /** The secretary instruction template text. Required on every call. */
  secretary_instructions: string;
  /** IANA timezone for the 08:00 daily brief. Omitted, the server uses the caller's saved timezone, then UTC. */
  timezone?: string;
}

/** The 409 body when setup needs an online Hermes runtime first. */
export interface OfficeSetupRefusal {
  code: string;
  hint: string;
  /** Path on the docs site that explains the install. */
  docs_path: string;
}

const DEFAULT_DOCS_PATH = "/install-agent-runtime";

export const OfficeSetupReadinessSchema = z
  .object({
    runtime_online: z.boolean().default(false),
    secretary_id: z
      .string()
      .nullable()
      .default(null)
      .transform((v) => (v ? v : null)),
    executive_floor: z.boolean().default(false),
    autopilot_daily: z.boolean().default(false),
    autopilot_weekly_paused: z.boolean().default(false),
    ready: z.boolean().default(false),
  })
  .loose();

/** What an unreadable response degrades to: nothing set up, so the card asks for setup. */
export const FALLBACK_OFFICE_SETUP_READINESS: OfficeSetupReadiness = {
  runtime_online: false,
  secretary_id: null,
  executive_floor: false,
  autopilot_daily: false,
  autopilot_weekly_paused: false,
  ready: false,
};

const OfficeSetupRefusalSchema = z
  .object({
    code: z.string(),
    hint: z.string().default(""),
    docs_path: z.string().default(DEFAULT_DOCS_PATH),
  })
  .loose();

/**
 * Whether the Secretary is set up and can run, from the individual checks. The
 * decision uses the checks rather than the server's `ready` flag alone, so one
 * flag cannot hide a missing piece.
 */
export function isOfficeSetupReady(readiness: OfficeSetupReadiness): boolean {
  return (
    readiness.runtime_online &&
    readiness.secretary_id !== null &&
    readiness.executive_floor &&
    readiness.autopilot_daily &&
    readiness.autopilot_weekly_paused
  );
}

/** The refusal behind a 409 from setup, or null for any other error body. */
export function parseOfficeSetupRefusal(body: unknown): OfficeSetupRefusal | null {
  const parsed = OfficeSetupRefusalSchema.safeParse(body);
  return parsed.success ? parsed.data : null;
}
