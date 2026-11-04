import { z } from "zod";

/**
 * A squad's adaptive attention (spec 2.6, `GET /api/squads/{id}/attention`).
 * pending_cap and weekly_cap are the effective limits; the computed ones are
 * what the nightly job chose, before any admin pin. reason says why.
 */
export const SQUAD_ATTENTION_REASONS = [
  "pinned",
  "sample_too_small",
  "below_threshold",
  "above_threshold",
  "default",
] as const;
export type SquadAttentionReason = (typeof SQUAD_ATTENTION_REASONS)[number] | "unknown";

/** Each cap the server stores is between 1 and this, computed or pinned. */
export const SQUAD_ATTENTION_MAX_CAP = 10;

export interface SquadAttention {
  squad_id: string;
  pending_cap: number;
  weekly_cap: number;
  computed_pending_cap: number;
  computed_weekly_cap: number;
  pinned: boolean;
  pinned_pending_cap: number | null;
  pinned_weekly_cap: number | null;
  /** Approved over decided in the window, 0 to 1; null when nothing was decided. */
  acceptance_rate: number | null;
  sample_size: number;
  reason: SquadAttentionReason;
  computed_at: string | null;
}

/** `PUT /api/squads/{id}/attention/pin`. A pin needs both caps; clearing ignores them. */
export interface PinSquadAttentionRequest {
  pinned: boolean;
  pending_cap?: number;
  weekly_cap?: number;
}

const CapSchema = z.number().int().default(0);

export const SquadAttentionSchema = z
  .object({
    squad_id: z.string().default(""),
    pending_cap: CapSchema,
    weekly_cap: CapSchema,
    computed_pending_cap: CapSchema,
    computed_weekly_cap: CapSchema,
    pinned: z.boolean().catch(false).default(false),
    pinned_pending_cap: z.number().int().nullable().catch(null).default(null),
    pinned_weekly_cap: z.number().int().nullable().catch(null).default(null),
    acceptance_rate: z.number().min(0).max(1).nullable().catch(null).default(null),
    sample_size: z.number().default(0),
    reason: z.enum([...SQUAD_ATTENTION_REASONS, "unknown"] as const).catch("unknown"),
    computed_at: z.string().nullable().catch(null).default(null),
  })
  .loose();
