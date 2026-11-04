import { z } from "zod";
import { parseWithFallback } from "../api/schema";

/**
 * The company pulse (spec 4.8): the company at a glance for the Company header
 * strip. The server caches it for a minute, so the strip polls no faster.
 */
export interface CompanyPulse {
  /** running, paused, quiet, or unknown for a state this client does not know. */
  state: string;
  decisions_waiting: number;
  /** -1 when no decision waits. */
  oldest_decision_age_seconds: number;
  goals_at_risk: number;
  budgets_above_soft_line: number;
  budget_holds: number;
  stalled_initiatives: number;
  routines_late: number;
  knowledge_retrieval_hit_rate: number | null;
  runtimes_offline: number;
  generated_at: string;
}

/** The digest source: the setting, the squads it is judged on and what it resolves to. */
export interface DigestMode {
  setting: string;
  active_squads: number;
  resolved: string;
  auto_floor_threshold: number;
}

export const CompanyPulseSchema = z
  .object({
    state: z.string().default("unknown"),
    decisions_waiting: z.number().default(0),
    oldest_decision_age_seconds: z.number().default(-1),
    goals_at_risk: z.number().default(0),
    budgets_above_soft_line: z.number().default(0),
    budget_holds: z.number().default(0),
    stalled_initiatives: z.number().default(0),
    routines_late: z.number().default(0),
    knowledge_retrieval_hit_rate: z.number().nullish().transform((v) => (v == null ? null : v)),
    runtimes_offline: z.number().default(0),
    generated_at: z.string().default(""),
  })
  .loose()
  .transform((pulse): CompanyPulse => pulse);

/** What a pulse response degrades to: the strip shows nothing it cannot read. */
export const EMPTY_COMPANY_PULSE: CompanyPulse = {
  state: "unknown",
  decisions_waiting: 0,
  oldest_decision_age_seconds: -1,
  goals_at_risk: 0,
  budgets_above_soft_line: 0,
  budget_holds: 0,
  stalled_initiatives: 0,
  routines_late: 0,
  knowledge_retrieval_hit_rate: null,
  runtimes_offline: 0,
  generated_at: "",
};

export const DigestModeSchema = z
  .object({
    setting: z.string().default("auto"),
    active_squads: z.number().default(0),
    resolved: z.string().default("squads"),
    auto_floor_threshold: z.number().default(30),
  })
  .loose()
  .transform((mode): DigestMode => mode);

export function parseCompanyPulse(raw: unknown, endpoint = "GET /api/company/pulse"): CompanyPulse {
  return parseWithFallback<CompanyPulse>(raw, CompanyPulseSchema, EMPTY_COMPANY_PULSE, { endpoint });
}
