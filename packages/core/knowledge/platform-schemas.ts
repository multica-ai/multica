import { z } from "zod";
import type {
  KnowledgeAccess,
  KnowledgeAccessLevel,
  KnowledgeBaseSettings,
  KnowledgeModelChecks,
  KnowledgeOwnerLogin,
  KnowledgeSources,
  KnowledgeSubjectType,
} from "./platform-types";
import type { KnowledgeState, KnowledgeVisibility } from "./types";

// A value a newer server adds, or one it leaves out, degrades to `unknown`. A
// grant or a source that cannot be read is never shown as working access.

const KNOWN_STATES = ["disabled", "provisioning", "ready", "failed", "offline"] as const;
const KNOWN_VISIBILITIES = ["workspace", "restricted"] as const;
const KNOWN_SUBJECTS = ["squad", "floor", "agent"] as const;
const KNOWN_ACCESS = ["read", "write"] as const;

const StateSchema = z
  .string()
  .default("unknown")
  .transform((s): KnowledgeState =>
    (KNOWN_STATES as readonly string[]).includes(s) ? (s as KnowledgeState) : "unknown",
  );

const VisibilitySchema = z
  .string()
  .default("unknown")
  .transform((s): KnowledgeVisibility =>
    (KNOWN_VISIBILITIES as readonly string[]).includes(s) ? (s as KnowledgeVisibility) : "unknown",
  );

const SubjectTypeSchema = z
  .string()
  .default("unknown")
  .transform((s): KnowledgeSubjectType | "unknown" =>
    (KNOWN_SUBJECTS as readonly string[]).includes(s) ? (s as KnowledgeSubjectType) : "unknown",
  );

const AccessSchema = z
  .string()
  .default("unknown")
  .transform((s): KnowledgeAccessLevel =>
    (KNOWN_ACCESS as readonly string[]).includes(s) ? (s as KnowledgeAccessLevel) : "unknown",
  );

export const KnowledgeSourceSchema = z
  .object({
    id: z.string().default(""),
    knowledge_base: z.string().default(""),
    name: z.string().default(""),
    type: z.string().default(""),
    status: z.string().default("unknown"),
    sync_schedule: z.string().default(""),
    last_sync_at: z.string().nullish().transform((v) => v ?? null),
    error_message: z.string().default(""),
    total_items_synced: z.number().default(0),
  })
  .loose();

export const KnowledgeSourcesSchema = z
  .object({
    state: StateSchema,
    ui_url: z.string().default(""),
    sources: z
      .array(KnowledgeSourceSchema)
      .nullish()
      .transform((v) => v ?? []),
  })
  .loose();

export const KnowledgeGrantSchema = z
  .object({
    subject_type: SubjectTypeSchema,
    subject_id: z.string().default(""),
    access: AccessSchema,
  })
  .loose();

export const KnowledgeAccessSchema = z
  .object({
    key: z.string().default(""),
    visibility: VisibilitySchema,
    grants: z
      .array(KnowledgeGrantSchema)
      .nullish()
      .transform((v) => v ?? []),
  })
  .loose();

export const KnowledgeBaseSettingsSchema = z
  .object({
    key: z.string().default(""),
    wiki_enabled: z.boolean().default(false),
    summaries_enabled: z.boolean().default(false),
  })
  .loose();

export const KnowledgeModelChecksSchema = z
  .object({
    configured: z.boolean().default(false),
    checks: z
      .array(
        z
          .object({
            role: z.string().default(""),
            name: z.string().default(""),
            ok: z.boolean().default(false),
            error: z.string().default(""),
          })
          .loose(),
      )
      .nullish()
      .transform((v) => v ?? []),
  })
  .loose();

export const KnowledgeOwnerLoginSchema = z
  .object({
    email: z.string().default(""),
    password: z.string().default(""),
    ui_url: z.string().default(""),
  })
  .loose();

/** Read as "unavailable": nothing is shown as an available source or grant. */
export const FALLBACK_KNOWLEDGE_SOURCES: KnowledgeSources = {
  state: "unknown",
  ui_url: "",
  sources: [],
};

export const FALLBACK_KNOWLEDGE_ACCESS: KnowledgeAccess = {
  key: "",
  visibility: "unknown",
  grants: [],
};

export const FALLBACK_KNOWLEDGE_BASE_SETTINGS: KnowledgeBaseSettings = {
  key: "",
  wiki_enabled: false,
  summaries_enabled: false,
};

export const FALLBACK_KNOWLEDGE_MODEL_CHECKS: KnowledgeModelChecks = {
  configured: false,
  checks: [],
};

/** A login that cannot be read is an empty login, which is never shown. */
export const FALLBACK_KNOWLEDGE_OWNER_LOGIN: KnowledgeOwnerLogin = {
  email: "",
  password: "",
  ui_url: "",
};
