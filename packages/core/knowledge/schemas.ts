import { z } from "zod";
import type {
  KnowledgeBases,
  KnowledgeDocument,
  KnowledgeDocumentList,
  KnowledgeDocumentState,
  KnowledgeSearchResult,
  KnowledgeState,
  KnowledgeStatus,
  KnowledgeVisibility,
} from "./types";

const KNOWN_STATES = ["disabled", "provisioning", "ready", "failed", "offline"] as const;
const KNOWN_DOCUMENT_STATES = ["draft", "published", "failed"] as const;
const KNOWN_VISIBILITIES = ["workspace", "restricted"] as const;

// A state a newer server adds, or one the server left out, degrades to
// "unknown" rather than to "ready": a view must never treat a state it cannot
// read as a working knowledge base.
const KnowledgeStateSchema = z
  .string()
  .default("unknown")
  .transform((s): KnowledgeState =>
    (KNOWN_STATES as readonly string[]).includes(s) ? (s as KnowledgeState) : "unknown",
  );

const DocumentStateSchema = z
  .string()
  .default("unknown")
  .transform((s): KnowledgeDocumentState =>
    (KNOWN_DOCUMENT_STATES as readonly string[]).includes(s) ? (s as KnowledgeDocumentState) : "unknown",
  );

const VisibilitySchema = z
  .string()
  .default("unknown")
  .transform((s): KnowledgeVisibility =>
    (KNOWN_VISIBILITIES as readonly string[]).includes(s) ? (s as KnowledgeVisibility) : "unknown",
  );

export const KnowledgeBaseSchema = z
  .object({
    id: z.string().default(""),
    key: z.string().default(""),
    name: z.string().default(""),
    visibility: VisibilitySchema,
    position: z.number().default(0),
    // Off unless the server says otherwise: a missing flag is never shown as on.
    wiki_enabled: z.boolean().default(false),
    summaries_enabled: z.boolean().default(false),
  })
  .loose();

export const KnowledgeBasesSchema = z
  .object({
    offered: z.boolean().default(false),
    state: KnowledgeStateSchema,
    knowledge_bases: z
      .array(KnowledgeBaseSchema)
      .nullish()
      .transform((v) => v ?? []),
  })
  .loose();

export const KnowledgeStatusSchema = z
  .object({
    offered: z.boolean().default(false),
    reason: z.string().default(""),
    enabled: z.boolean().default(false),
    state: KnowledgeStateSchema,
    knowledge_bases: z
      .array(KnowledgeBaseSchema)
      .nullish()
      .transform((v) => v ?? []),
    ui_url: z.string().default(""),
  })
  .loose();

export const KnowledgeDocumentSchema = z
  .object({
    id: z.string().default(""),
    document_id: z.string().default(""),
    knowledge_base: z.string().default(""),
    title: z.string().default(""),
    state: DocumentStateSchema,
    kind: z.string().default(""),
    url: z.string().default(""),
    source_type: z.string().default(""),
    source_id: z.string().nullable().default(null),
    created_by_type: z.string().default(""),
    created_by_id: z.string().nullable().default(null),
    created_at: z.string().default(""),
    published_at: z.string().nullable().default(null),
    error: z.string().default(""),
  })
  .loose();

export const KnowledgeDocumentListSchema = z
  .object({
    state: KnowledgeStateSchema,
    documents: z
      .array(KnowledgeDocumentSchema)
      .nullish()
      .transform((v) => v ?? []),
  })
  .loose();

export const KnowledgeHitSchema = z
  .object({
    document_id: z.string().default(""),
    knowledge_base: z.string().default(""),
    title: z.string().default(""),
    content: z.string().default(""),
    source: z.string().default(""),
    score: z.number().default(0),
  })
  .loose();

export const KnowledgeSearchResultSchema = z
  .object({
    state: KnowledgeStateSchema,
    hits: z
      .array(KnowledgeHitSchema)
      .nullish()
      .transform((v) => v ?? []),
  })
  .loose();

/** An unreadable bases response: not offered, so nothing is shown as usable. */
export const FALLBACK_KNOWLEDGE_BASES: KnowledgeBases = {
  offered: false,
  state: "unknown",
  knowledge_bases: [],
};

export const FALLBACK_KNOWLEDGE_STATUS: KnowledgeStatus = {
  offered: false,
  reason: "",
  enabled: false,
  state: "unknown",
  knowledge_bases: [],
  ui_url: "",
};

export const FALLBACK_KNOWLEDGE_DOCUMENT_LIST: KnowledgeDocumentList = {
  state: "unknown",
  documents: [],
};

export const FALLBACK_KNOWLEDGE_SEARCH: KnowledgeSearchResult = {
  state: "unknown",
  hits: [],
};

/** An unreadable single-document response: an id-less, `unknown` row. */
export const FALLBACK_KNOWLEDGE_DOCUMENT: KnowledgeDocument = {
  id: "",
  document_id: "",
  knowledge_base: "",
  title: "",
  state: "unknown",
  kind: "",
  url: "",
  source_type: "",
  source_id: null,
  created_by_type: "",
  created_by_id: null,
  created_at: "",
  published_at: null,
  error: "",
};
