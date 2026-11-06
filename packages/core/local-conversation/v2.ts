import { z } from "zod";
import { localSubmissionSchema } from "./submission";

/**
 * Local conversation protocol v2 between Desktop and the Go daemon
 * (`/local/v2`). The definition source is
 * server/internal/localconversation/protocol/schema/local-v2.schema.json;
 * these schemas decode the same golden samples. They accept unknown event
 * types and enum values from a newer daemon: callers skip what they do not
 * understand instead of failing the connection.
 */

export const LOCAL_V2_VERSION = 2;

const nullableString = z.string().nullable().optional().transform((value) => value ?? null);
const payload = z.record(z.string(), z.unknown());

export const localV2EventSchema = z.object({
  seq: z.number().int().positive(),
  turn_id: nullableString,
  turn_seq: z.number().int().nullable().optional().transform((value) => value ?? null),
  item_id: nullableString,
  type: z.string(),
  final: z.boolean(),
  created_at: z.string(),
  /** Sequence of the item's first event, on the final event a settled turn is read back from. */
  first_seq: z.number().int().optional(),
  first_at: z.string().optional(),
  payload,
});
export type LocalV2Event = z.infer<typeof localV2EventSchema>;

export const localV2SummarySchema = z.object({
  conversation_id: z.string(),
  summary_seq: z.number().int(),
  provider: z.string(),
  title: z.string(),
  status: z.string(),
  cwd: z.string().default(""),
  project_id: nullableString,
  pinned: z.boolean(),
  archived: z.boolean(),
  last_message_preview: z.string().default(""),
  pending_interactions: z.number().int().default(0),
  queue_count: z.number().int().default(0),
  /** Background tasks still running after their turn. */
  background_tasks: z.number().int().default(0),
  side_source_id: z.string().optional(),
  side_source_turn_id: z.string().optional(),
  side_quote: z.string().optional(),
  active_turn_id: z.string().optional(),
  turn_count: z.number().int().default(0),
  created_at: z.string().optional(),
  updated_at: z.string(),
  /** Latest user turn's send time; null before any user turn, absent on older daemons. */
  last_turn_sent_at: z.string().datetime({ offset: true }).nullable().optional(),
});
export type LocalV2Summary = z.infer<typeof localV2SummarySchema>;

const errorSchema = z.object({ kind: z.string(), message: z.string() });

export const localV2ServerMessageSchema = z.discriminatedUnion("type", [
  z.object({
    version: z.literal(LOCAL_V2_VERSION), type: z.literal("conversation.events"), conversation_id: z.string(),
    from_seq: z.number().int(), to_seq: z.number().int(), events: z.array(localV2EventSchema).min(1),
  }),
  z.object({ version: z.literal(LOCAL_V2_VERSION), type: z.literal("scope.summaries"), scope_seq: z.number().int(), summaries: z.array(localV2SummarySchema) }),
  z.object({
    version: z.literal(LOCAL_V2_VERSION), type: z.literal("subscription.status"), conversation_id: z.string().optional(),
    status: z.string(), high_water: z.number().int().optional(), error: errorSchema.optional(),
  }),
  z.object({ version: z.literal(LOCAL_V2_VERSION), type: z.literal("host.request"), request_id: z.string(), kind: z.string(), payload }),
  // Other devices using a conversation now; an empty list clears it.
  z.object({ version: z.literal(LOCAL_V2_VERSION), type: z.literal("scope.presence"), conversation_id: z.string().min(1), devices: z.array(z.object({ peer_id: z.string().min(1), device_name: z.string() })) }),
]);
export type LocalV2ServerMessage = z.infer<typeof localV2ServerMessageSchema>;
/** Another device using a conversation, as presence messages name it. */
export interface LocalV2Device { peer_id: string; device_name: string }

export const localV2ClientMessageSchema = z.discriminatedUnion("type", [
  z.object({ version: z.literal(LOCAL_V2_VERSION), type: z.literal("subscribe_scope"), after_scope_seq: z.number().int().min(0).optional(), store_generation: z.string().optional() }).strict(),
  z.object({ version: z.literal(LOCAL_V2_VERSION), type: z.literal("subscribe"), conversation_id: z.string(), after_seq: z.number().int().min(0), store_generation: z.string() }).strict(),
  z.object({ version: z.literal(LOCAL_V2_VERSION), type: z.literal("unsubscribe"), conversation_id: z.string() }).strict(),
  z.object({ version: z.literal(LOCAL_V2_VERSION), type: z.literal("conversation.focus"), conversation_id: z.string(), focused: z.boolean() }).strict(),
  z.object({ version: z.literal(LOCAL_V2_VERSION), type: z.literal("host.reply"), request_id: z.string(), result: z.unknown().optional(), error: errorSchema.optional() }).strict(),
]);
export type LocalV2ClientMessage = z.infer<typeof localV2ClientMessageSchema>;

export const localV2HelloSchema = z.object({
  version: z.number().int(),
  store_generation: z.string(),
  capabilities: z.array(z.string()).default([]),
  scope_seq: z.number().int().nonnegative().optional(),
});

const snapshotMessageSchema = z.object({
  id: z.string(), role: z.string(), content: z.string(),
  attachment_ids: z.array(z.string()).nullable().default([]).transform((value) => value ?? []),
  steered: z.boolean().default(false), failed: z.boolean().default(false), created_at: z.string(),
});

const snapshotTurnSchema = z.object({
  id: z.string(), turn_index: z.number().int(), request_id: z.string().default(""), status: z.string(),
  started_at: z.string(), completed_at: z.string().optional(), error: errorSchema.optional(),
  messages: z.array(snapshotMessageSchema), events: z.array(localV2EventSchema),
});
export type LocalV2SnapshotTurn = z.infer<typeof snapshotTurnSchema>;

export const localV2SnapshotSchema = z.object({
  version: z.number().int(),
  store_generation: z.string(),
  snapshot_seq: z.number().int(),
  conversation: localV2SummarySchema,
  turns: z.array(snapshotTurnSchema),
  has_older_turns: z.boolean().default(false),
  pending_interactions: z
    .array(
      z.object({
        interaction_id: z.string(),
        turn_id: z.string(),
        kind: z.string(),
        request: payload,
        created_at: z.string(),
      }),
    )
    .default([]),
  settings: z
    .object({
      model: nullableString,
      reasoning_effort: nullableString,
      permission_mode: z.string().default("default"),
    })
    .default({
      model: null,
      reasoning_effort: null,
      permission_mode: "default",
    }),
  submissions: z.array(localSubmissionSchema).default([]),
  queue: z
    .object({
      revision: z.number().int(),
      paused: z.boolean(),
      error: z.string().optional(),
      dispatching_request_id: z.string().optional(),
      items: z.array(payload).default([]),
    })
    .default({ revision: 0, paused: false, items: [] }),
  /** Events of background items still running whose turn is older than this snapshot's window. */
  open_background_items: z.array(localV2EventSchema).default([]),
  created_at: z.string().default(""),
  forked_from_id: z.string().default(""),
  forked_from_turn_id: z.string().default(""),
  imported_from_session_id: z.string().default(""),
});
export type LocalV2Snapshot = z.infer<typeof localV2SnapshotSchema>;

export const localV2QueueSchema = z.object({
  revision: z.number().int(), paused: z.boolean(), error: z.string().optional(), dispatching_request_id: z.string().optional(),
  items: z.array(payload).default([]),
});
export type LocalV2Queue = z.infer<typeof localV2QueueSchema>;

export const localSubmissionResultSchema = z.object({
  store_generation: z.string(),
  snapshot_seq: z.number().int().nonnegative(),
  queue_revision: z.number().int().nonnegative(),
  submission: localSubmissionSchema,
  queue: localV2QueueSchema,
});
export type LocalSubmissionResult = z.infer<typeof localSubmissionResultSchema>;

/** A daemon announcing this can say what became of queued messages. */
export const LOCAL_V2_PROMPT_STATUS_CAPABILITY = "prompt-status-v1";

export const localV2PromptStatusesSchema = z.object({
  statuses: z.record(z.string(), z.enum(["queued", "dispatching", "delivered", "failed", "unknown", "removed", "none"]).catch("none")).default({}),
});

export const localV2HistorySchema = z.object({
  store_generation: z.string(),
  turns: z.array(snapshotTurnSchema),
  has_older_turns: z.boolean(),
});
export type LocalV2History = z.infer<typeof localV2HistorySchema>;

const navigationRangeSchema = z.object({
  start_index: z.number().int().nonnegative(), end_index: z.number().int().nonnegative(),
  has_before: z.boolean(), has_after: z.boolean(),
}).refine((value) => value.end_index >= value.start_index, "Invalid navigation range");

export const localV2NavigationSchema = z.object({
  store_generation: z.string(),
  snapshot_seq: z.number().int().nonnegative(),
  node_count: z.number().int().nonnegative(),
  nodes: z.array(z.object({
    node_index: z.number().int().nonnegative(), message_id: z.string().min(1),
    turn_id: z.string().min(1), turn_index: z.number().int().nonnegative(),
    summary: z.string(), has_agent_response: z.boolean().default(false),
  })),
  directory_range: navigationRangeSchema.nullable().optional(),
  range: navigationRangeSchema.nullable(),
  turns: z.array(snapshotTurnSchema).max(20),
}).superRefine((value, ctx) => {
  if (value.range && (value.range.end_index < value.range.start_index || value.turns.length !== value.range.end_index - value.range.start_index + 1 || value.turns.some((turn, index) => turn.turn_index !== value.range!.start_index + index))) {
    ctx.addIssue({ code: "custom", message: "Navigation requires a contiguous body window" });
  }
  if ((!value.range && value.turns.length) || value.nodes.some((node) => node.node_index >= value.node_count)) {
    ctx.addIssue({ code: "custom", message: "Invalid navigation directory" });
  }
  if (value.directory_range && value.nodes.some((node) => node.turn_index < value.directory_range!.start_index || node.turn_index > value.directory_range!.end_index)) {
    ctx.addIssue({ code: "custom", message: "Node outside navigation directory range" });
  }
});
export type LocalV2Navigation = z.infer<typeof localV2NavigationSchema>;
export const LOCAL_V2_NAVIGATION_CAPABILITY = "conversation-navigation-v1";

/** Shared by desktop and mobile; device routing and authentication belong to their transports. */
export type LocalV2NavigationQuery = (
  | { nodeIndex: number; before?: number; after?: number }
  | { turnIndex: number; before?: number; after?: number }
  | { beforeIndex: number; limit?: number }
  | { afterIndex: number; limit?: number }
  | { nodeIndex?: never; turnIndex?: never; beforeIndex?: never; afterIndex?: never }
) & { generation?: string; includeBody?: boolean };

export function localV2NavigationPath(id: string, query: LocalV2NavigationQuery = {}): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined) continue;
    const name = key === "generation" ? "store_generation" : key.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`);
    params.set(name, String(value));
  }
  const suffix = params.toString();
  return `/sessions/${encodeURIComponent(id)}/navigation${suffix ? `?${suffix}` : ""}`;
}

export const localV2SessionListSchema = z.object({
  scope_seq: z.number().int(),
  summaries: z.array(localV2SummarySchema),
  has_more: z.boolean().default(false),
});

export const localV2SessionGroupsSchema = z.object({
  scope_seq: z.number().int().nonnegative(),
  groups: z.array(z.object({ id: z.string(), title: z.string(), path: z.string(), total: z.number().int().nonnegative() })),
});
export type LocalV2SessionGroup = z.infer<typeof localV2SessionGroupsSchema>["groups"][number];

export const localV2SessionPageSchema = z.object({
  scope_seq: z.number().int().nonnegative(),
  summaries: z.array(localV2SummarySchema),
  has_more: z.boolean(),
  next_cursor: z.string().min(1).nullable(),
}).refine((page) => page.has_more === (page.next_cursor !== null), { message: "A further session page requires a cursor" });
export type LocalV2SessionPage = z.infer<typeof localV2SessionPageSchema>;

export const localV2ErrorSchema = z.object({ error: errorSchema });

export interface LocalV2Scope { userId: string; workspaceId: string }

/**
 * Desktop's v2 transport as the renderer sees it: HTTP and one WebSocket per
 * scope straight to the daemon, with the OS picker left to the main process.
 */
export interface LocalV2Transport {
  /** A JSON request to `/local/v2{path}`. */
  request(scope: LocalV2Scope, method: "GET" | "POST" | "DELETE", path: string, body?: unknown): Promise<unknown>;
  /** Sends a client message on the scope's WebSocket. */
  send(scope: LocalV2Scope, message: LocalV2ClientMessage): Promise<void>;
  /** What the daemon behind this transport announces in /hello. */
  capabilities?(): Promise<string[]>;
  /** The daemon's store generation from /hello, read fresh. */
  storeGeneration?(): Promise<string>;
  /** Stops keeping a scope connected. */
  release?(scope: LocalV2Scope): void;
  /** Receives the scope's server messages. */
  subscribe(listener: (scope: LocalV2Scope, message: unknown) => void): () => void;
  /** Receives connection changes; subscriptions are lost on disconnect. */
  onConnection(listener: (scope: LocalV2Scope, connected: boolean) => void): () => void;
  /** A URL streaming a daemon file: an attachment, a conversation image or preview content. */
  fileUrl(scope: LocalV2Scope, path: string): string;
  /**
   * Uploads a message attachment; the result is parsed with localV2UploadSchema.
   * id names the attachment up front on daemons announcing attachment-reserve-v1.
   */
  upload(scope: LocalV2Scope, file: File, options?: { id?: string; signal?: AbortSignal }): Promise<unknown>;
  /**
   * Asks the user for a path and applies it; null when cancelled. Paths that
   * widen what the daemon may read come only from the OS picker.
   */
  choose(scope: LocalV2Scope, request: LocalV2ChooseRequest): Promise<unknown>;
}

/**
 * What Desktop's main process gives the renderer to reach the daemon: its
 * loopback address and credentials derived from the profile token, never
 * the token itself.
 */
export interface LocalV2RendererEndpoint { base: string; apiUrl: string; token: string; fileKey: string }

export type LocalV2ChooseRequest =
  | { purpose: "preview"; kind: "file" | "directory" }
  | { purpose: "folder_project"; defaultPath?: string }
  | { purpose: "conversation_directory"; conversationId: string; defaultPath?: string };

export const localV2UploadSchema = z.object({ id: z.string(), name: z.string(), mime_type: z.string(), size: z.number() });
export type LocalV2Upload = z.infer<typeof localV2UploadSchema>;

/** A page of the provider's native history that can be imported. */
export const localV2ImportPageSchema = z.object({
  sessions: z.array(z.object({
    id: z.string(), provider: z.string(), title: z.string().default(""), cwd: z.string().default(""),
    created_at: z.string().default(""), updated_at: z.string().default(""), imported_id: z.string().optional(),
  })).default([]),
  next_cursor: z.string().nullable().default(null),
});
export type LocalV2ImportPage = z.infer<typeof localV2ImportPageSchema>;

const importOutcomeSchema = z.object({ source_id: z.string(), conversation_id: z.string().default(""), error: z.string().default("") });
export const localV2ImportResultSchema = z.object({
  imported: z.array(importOutcomeSchema).default([]), skipped: z.array(importOutcomeSchema).default([]), failed: z.array(importOutcomeSchema).default([]),
});
export type LocalV2ImportResult = z.infer<typeof localV2ImportResultSchema>;
