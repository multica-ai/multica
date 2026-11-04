import { z } from "zod";

/**
 * A member's brief push preference (`GET` and `PUT /api/company/brief-push`).
 * quiet_hours is the window [start, end) in the member's local hours; it wraps
 * midnight when start is after end. Null means a push may arrive at any hour.
 * The server clears any field a write omits, so a save always sends all three.
 */
export interface BriefPushQuietHours {
  start: number;
  end: number;
}

export interface BriefPushPreference {
  brief_push: boolean;
  /** The member's chat binding that briefs go to; null until a chat is linked. */
  binding_id: string | null;
  quiet_hours: BriefPushQuietHours | null;
}

export type UpdateBriefPushRequest = BriefPushPreference;

/** What a member who has never set a preference reads: off, no chat, no quiet hours. */
export const EMPTY_BRIEF_PUSH: BriefPushPreference = {
  brief_push: false,
  binding_id: null,
  quiet_hours: null,
};

const HourSchema = z.number().int().min(0).max(23);

const QuietHoursSchema = z
  .object({ start: HourSchema, end: HourSchema })
  .loose();

// Lenient per field: a malformed quiet_hours reads as none, so the preference still loads.
export const BriefPushPreferenceSchema = z
  .object({
    brief_push: z.boolean().catch(false).default(false),
    binding_id: z.string().nullable().catch(null).default(null),
    quiet_hours: QuietHoursSchema.nullable().catch(null).default(null),
  })
  .loose();

/** A chat the caller can send briefs to (`GET /api/company/brief-push/bindings`). */
export interface BriefPushBinding {
  id: string;
  /** The chat app, lowercase: "telegram". */
  channel: string;
  /** The chat's name as the server shows it. */
  label: string;
}

const BriefPushBindingSchema = z
  .object({
    id: z.string().min(1),
    channel: z.string().min(1),
    label: z.string().catch(""),
  })
  .loose();

// Each entry is checked alone: one malformed row is dropped and the rest still list.
export const BriefPushBindingListSchema = z.array(z.unknown()).transform((items) =>
  items.flatMap((item) => {
    const parsed = BriefPushBindingSchema.safeParse(item);
    return parsed.success ? [parsed.data] : [];
  }),
);
