import { z } from "zod";
import { parseWithFallback } from "../api/schema";

/**
 * System health (spec 4.9), as `GET /api/governance/health` returns it. A
 * reading the server could not take is absent or null, and views say so rather
 * than showing it as healthy.
 */
export interface ScheduledJobStatus {
  name: string;
  label: string;
  cadence_seconds: number;
  /** ISO time of the last success, or null when the log has none. */
  last_success_at: string | null;
  last_failed_at: string | null;
  /** Seconds since the last success; -1 when there is none. */
  lag_seconds: number;
  stuck: boolean;
  /** Why the job is stuck, in words. Empty when it is not. */
  stuck_reason: string;
}

export interface KnowledgeHealth {
  offered: boolean;
  /** `ready`, `offline`, `provisioning`, `failed`, `disabled`, or `unknown` for a state this client does not know. */
  state: string;
  reason: string;
}

export interface BudgetGateHealth {
  /** False until the budget gate reports its cache. */
  reported: boolean;
  cache_age_seconds: number | null;
}

export interface HealthReport {
  checked_at: string;
  jobs: ScheduledJobStatus[];
  delivery_failures: number;
  knowledge: KnowledgeHealth;
  budget_gate: BudgetGateHealth;
  risks: string[];
}

const jobSchema = z
  .object({
    name: z.string().default(""),
    label: z.string().default(""),
    cadence_seconds: z.number().default(0),
    last_success_at: z.string().nullish().transform((v) => v || null),
    last_failed_at: z.string().nullish().transform((v) => v || null),
    lag_seconds: z.number().default(-1),
    stuck: z.boolean().default(false),
    stuck_reason: z.string().default(""),
  })
  .loose()
  .transform((job): ScheduledJobStatus => job);

export const HealthReportSchema = z
  .object({
    checked_at: z.string().default(""),
    jobs: z.array(jobSchema).default([]),
    delivery_failures: z.number().default(0),
    knowledge: z
      .object({
        offered: z.boolean().default(false),
        state: z.string().default("unknown"),
        reason: z.string().default(""),
      })
      .loose()
      .default({ offered: false, state: "unknown", reason: "" }),
    budget_gate: z
      .object({
        reported: z.boolean().default(false),
        cache_age_seconds: z.number().nullish().transform((v) => (v == null ? null : v)),
      })
      .loose()
      .default({ reported: false, cache_age_seconds: null }),
    risks: z.array(z.string()).default([]),
  })
  .loose()
  .transform((report): HealthReport => report);

/** What a health response degrades to: nothing is known, so nothing is claimed. */
export const EMPTY_HEALTH_REPORT: HealthReport = {
  checked_at: "",
  jobs: [],
  delivery_failures: 0,
  knowledge: { offered: false, state: "unknown", reason: "" },
  budget_gate: { reported: false, cache_age_seconds: null },
  risks: [],
};

export function parseHealthReport(raw: unknown, endpoint = "GET /api/governance/health"): HealthReport {
  return parseWithFallback<HealthReport>(raw, HealthReportSchema, EMPTY_HEALTH_REPORT, { endpoint });
}
