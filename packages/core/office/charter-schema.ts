import { z } from "zod";

/**
 * A squad's charter and routines (`/api/squads/{id}/charter` and
 * `/api/squads/{id}/routines`). Enums the server may extend are read with an
 * `unknown` fallback, so a state or kind from a newer server renders as a
 * generic value instead of blanking the panel. Only the four output kinds the
 * server accepts can be saved; an `unknown` kind is sent back as-is and the
 * server refuses it with a message.
 */

export const CHARTER_TEXT_MAX = 2000;
export const CHARTER_ITEM_TEXT_MAX = 500;
export const CHARTER_KEY_MAX = 64;
export const CHARTER_KB_KEY_MAX = 128;
export const CHARTER_CADENCE_HINT_MAX = 64;
export const CHARTER_LIST_MAX = 20;
export const CHARTER_DEFAULT_RESPONSE_HOURS = 48;
export const CHARTER_RESPONSE_HOURS_MIN = 1;
export const CHARTER_RESPONSE_HOURS_MAX = 720;

export const ROUTINE_STATES = ["on_time", "no_output", "late", "failed", "paused", "never_ran"] as const;
export type RoutineState = (typeof ROUTINE_STATES)[number] | "unknown";

export const CHARTER_OUTPUT_KINDS = ["issue", "proposal", "kb_document", "report"] as const;
export type CharterOutputKind = (typeof CHARTER_OUTPUT_KINDS)[number] | "unknown";

export const ROUTINE_EXPECTED_OUTPUTS = ["none", ...CHARTER_OUTPUT_KINDS] as const;
export type RoutineExpectedOutput = (typeof ROUTINE_EXPECTED_OUTPUTS)[number] | "unknown";

/** The cadence presets the server turns into cron expressions. */
export const ROUTINE_CADENCES = ["daily", "weekdays", "weekly", "biweekly", "monthly"] as const;
export type RoutineCadence = (typeof ROUTINE_CADENCES)[number];

export interface SquadCharterResponsibility {
  key: string;
  text: string;
}

export interface SquadCharterOutput {
  key: string;
  kind: CharterOutputKind;
  kb_key: string;
  cadence_hint: string;
}

/** An output as the save request sends it: the kind must be one the server accepts. */
export interface SquadCharterOutputInput {
  key: string;
  kind: Exclude<CharterOutputKind, "unknown">;
  kb_key: string;
  cadence_hint: string;
}

export interface SquadCharter {
  squad_id: string;
  mission: string;
  responsibilities: SquadCharterResponsibility[];
  outputs: SquadCharterOutput[];
  boundaries: string;
  escalation: string;
  intake: string;
  response_hours: number;
  operating_review_enabled: boolean;
  /** 0 when the squad has no charter yet. */
  revision: number;
  updated_by_type: string;
  updated_by_id: string | null;
  updated_at: string | null;
}

/**
 * The save body. `revision` is the revision the editor read; the server answers
 * a stale one with a 409 (see `parseCharterRevisionConflict`). Omitting
 * `operating_review_enabled` keeps the stored switch.
 */
export interface UpdateSquadCharterRequest {
  revision: number;
  mission: string;
  responsibilities: SquadCharterResponsibility[];
  outputs: SquadCharterOutputInput[];
  boundaries: string;
  escalation: string;
  intake: string;
  response_hours: number;
  operating_review_enabled?: boolean;
}

export interface SquadRoutineSchedule {
  cron_expression: string;
  timezone: string;
}

export interface SquadRoutineLastRun {
  id: string;
  status: string;
  source: string;
  triggered_at: string;
  completed_at: string | null;
}

/** A routine is an autopilot linked to a squad. `autopilot_id` identifies it. */
export interface SquadRoutine {
  autopilot_id: string;
  squad_id: string;
  autopilot_title: string;
  autopilot_status: string;
  purpose: string;
  expected_output: RoutineExpectedOutput;
  required: boolean;
  grace_hours: number;
  state: RoutineState;
  schedule: SquadRoutineSchedule | null;
  next_run_at: string | null;
  last_run: SquadRoutineLastRun | null;
  created_at: string;
}

/**
 * Creates a routine. Send `autopilot_id` to link an existing autopilot, or
 * `cadence` (a preset or a cron expression) to create one. Not both.
 */
export interface CreateSquadRoutineRequest {
  autopilot_id?: string;
  cadence?: string;
  timezone?: string;
  purpose: string;
  description?: string;
  expected_output?: Exclude<RoutineExpectedOutput, "unknown">;
  required?: boolean;
  grace_hours?: number;
}

/** Every field defaults so an older or newer server never blanks the charter. */
function listOf<T extends z.ZodType>(item: T) {
  return z
    .array(item)
    .nullable()
    .default([])
    .transform((v) => v ?? []);
}

export const SquadCharterResponsibilitySchema = z
  .object({
    key: z.string().default(""),
    text: z.string().default(""),
  })
  .loose();

export const SquadCharterOutputSchema = z
  .object({
    key: z.string().default(""),
    kind: z.enum([...CHARTER_OUTPUT_KINDS, "unknown"] as const).catch("unknown"),
    kb_key: z.string().default(""),
    cadence_hint: z.string().default(""),
  })
  .loose();

export const SquadCharterSchema = z
  .object({
    squad_id: z.string().default(""),
    mission: z.string().default(""),
    responsibilities: listOf(SquadCharterResponsibilitySchema),
    outputs: listOf(SquadCharterOutputSchema),
    boundaries: z.string().default(""),
    escalation: z.string().default(""),
    intake: z.string().default(""),
    response_hours: z.number().default(CHARTER_DEFAULT_RESPONSE_HOURS),
    operating_review_enabled: z.boolean().default(true),
    revision: z.number().default(0),
    updated_by_type: z.string().default("member"),
    updated_by_id: z.string().nullable().default(null),
    updated_at: z.string().nullable().default(null),
  })
  .loose();

/** The charter a squad with none written yet reads as, at revision 0. */
export function emptySquadCharter(squadId: string): SquadCharter {
  return {
    squad_id: squadId,
    mission: "",
    responsibilities: [],
    outputs: [],
    boundaries: "",
    escalation: "",
    intake: "",
    response_hours: CHARTER_DEFAULT_RESPONSE_HOURS,
    operating_review_enabled: true,
    revision: 0,
    updated_by_type: "member",
    updated_by_id: null,
    updated_at: null,
  };
}

export const SquadRoutineSchema = z
  .object({
    autopilot_id: z.string(),
    squad_id: z.string().default(""),
    autopilot_title: z.string().default(""),
    autopilot_status: z.string().default(""),
    purpose: z.string().default(""),
    expected_output: z.enum([...ROUTINE_EXPECTED_OUTPUTS, "unknown"] as const).catch("unknown"),
    required: z.boolean().default(true),
    grace_hours: z.number().default(24),
    state: z.enum([...ROUTINE_STATES, "unknown"] as const).catch("unknown"),
    schedule: z
      .object({
        cron_expression: z.string().default(""),
        timezone: z.string().default("UTC"),
      })
      .loose()
      .nullable()
      .default(null),
    next_run_at: z.string().nullable().default(null),
    last_run: z
      .object({
        id: z.string().default(""),
        status: z.string().default(""),
        source: z.string().default(""),
        triggered_at: z.string().default(""),
        completed_at: z.string().nullable().default(null),
      })
      .loose()
      .nullable()
      .default(null),
    created_at: z.string().default(""),
  })
  .loose();

export const SquadRoutineListSchema = listOf(SquadRoutineSchema);

/** What a routine response degrades to when it cannot be read. */
export const EMPTY_SQUAD_ROUTINE: SquadRoutine = {
  autopilot_id: "",
  squad_id: "",
  autopilot_title: "",
  autopilot_status: "",
  purpose: "",
  expected_output: "none",
  required: true,
  grace_hours: 24,
  state: "unknown",
  schedule: null,
  next_run_at: null,
  last_run: null,
  created_at: "",
};

const CharterRevisionConflictBodySchema = z
  .object({
    code: z.literal("charter_revision_conflict"),
    revision: z.number().default(0),
  })
  .loose();

/**
 * Reads the current revision out of a refused save, or null when the error is
 * not a revision conflict. `body` is `ApiError.body`.
 */
export function parseCharterRevisionConflict(body: unknown): number | null {
  const parsed = CharterRevisionConflictBodySchema.safeParse(body);
  return parsed.success ? parsed.data.revision : null;
}
