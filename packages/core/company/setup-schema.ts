import { z } from "zod";

/**
 * The Secretary setup (`GET` and `POST /api/company/setup`). The readiness
 * checklist drives the first-run card. Every field defaults, so an older or
 * newer server never blanks it, and an unreadable body reads as "not set up"
 * rather than as ready.
 */

export interface CompanySetupReadiness {
  /** An online Hermes runtime the caller may use is in the workspace. */
  runtime_online: boolean;
  /** The workspace's live Secretary agent, or null when none is named. */
  secretary_id: string | null;
  /** The "Secretary desk" project the Secretary files its requests in, or null until setup makes it. */
  secretary_desk_project_id: string | null;
  /** A floor named Executive exists. */
  executive_floor: boolean;
  /** The Secretary's daily brief autopilot exists and is active. */
  autopilot_daily: boolean;
  /** The Secretary's weekly review autopilot exists and is paused. */
  autopilot_weekly_paused: boolean;
  /** The Secretary runs the starter instructions. False when the boss has edited them or the template has moved on. */
  secretary_instructions_current: boolean;
  /** The server's own verdict. Use isCompanySetupReady for the decision. */
  ready: boolean;
}

export interface CompanySetupRequest {
  /** The secretary instruction template text. Required on every call. */
  secretary_instructions: string;
  /** IANA timezone for the 08:00 daily brief. Omitted, the server uses the caller's saved timezone, then UTC. */
  timezone?: string;
  /** Replace an existing Secretary's instructions with the template. Only on the boss's explicit choice. */
  update_secretary_instructions?: boolean;
}

/** The 409 body when setup needs an online Hermes runtime first. */
export interface CompanySetupRefusal {
  code: string;
  hint: string;
  /** Path on the docs site that explains the install. */
  docs_path: string;
}

/** The install guide's path on the docs site, used when a refusal omits its own. */
export const DEFAULT_COMPANY_SETUP_DOCS_PATH = "/install-agent-runtime";

export const CompanySetupReadinessSchema = z
  .object({
    runtime_online: z.boolean().default(false),
    secretary_id: z
      .string()
      .nullable()
      .default(null)
      .transform((v) => (v ? v : null)),
    secretary_desk_project_id: z
      .string()
      .nullable()
      .default(null)
      .transform((v) => (v ? v : null)),
    executive_floor: z.boolean().default(false),
    autopilot_daily: z.boolean().default(false),
    autopilot_weekly_paused: z.boolean().default(false),
    secretary_instructions_current: z.boolean().default(true),
    ready: z.boolean().default(false),
  })
  .loose();

/** What an unreadable response degrades to: nothing set up, so the card asks for setup. */
export const FALLBACK_COMPANY_SETUP_READINESS: CompanySetupReadiness = {
  runtime_online: false,
  secretary_id: null,
  secretary_desk_project_id: null,
  executive_floor: false,
  autopilot_daily: false,
  autopilot_weekly_paused: false,
  secretary_instructions_current: true,
  ready: false,
};

const CompanySetupRefusalSchema = z
  .object({
    code: z.string(),
    hint: z.string().default(""),
    docs_path: z.string().default(DEFAULT_COMPANY_SETUP_DOCS_PATH),
  })
  .loose();

/**
 * Whether the Secretary is set up and can run, from the individual checks. The
 * decision uses the checks rather than the server's `ready` flag alone, so one
 * flag cannot hide a missing piece.
 */
export function isCompanySetupReady(readiness: CompanySetupReadiness): boolean {
  return (
    readiness.runtime_online &&
    readiness.secretary_id !== null &&
    readiness.executive_floor &&
    readiness.autopilot_daily &&
    readiness.autopilot_weekly_paused
  );
}

/** The refusal behind a 409 from setup, or null for any other error body. */
export function parseCompanySetupRefusal(body: unknown): CompanySetupRefusal | null {
  const parsed = CompanySetupRefusalSchema.safeParse(body);
  return parsed.success ? parsed.data : null;
}
