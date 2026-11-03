import { z } from "zod";

/** Daemon <-> Claude adapter protocol; see server/internal/localconversation/protocol/schema/claude-adapter-v1.schema.json. */
export const CLAUDE_ADAPTER_PROTOCOL_VERSION = 1;

/** Announced in ready; a daemon uses prewarm and claim only when both are listed. */
export const CLAUDE_ADAPTER_CAPABILITIES = ["set_model", "set_permission_mode", "apply_flag_settings", "set_mcp_servers", "stop_task", "prewarm", "claim"];

const id = z.string().min(1).max(200);
const request = { version: z.literal(CLAUDE_ADAPTER_PROTOCOL_VERSION), id };
const permissionMode = z.enum(["default", "auto", "full-access"]);
const objectRecord = z.record(z.string(), z.unknown());

export const daemonMessageSchema = z.discriminatedUnion("type", [
  z.strictObject({
    ...request, type: z.literal("start"), executable: z.string().min(1), cwd: z.string(),
    resume: z.string().optional(), model: z.string().nullable().optional(), reasoning_effort: z.string().nullable().optional(),
    permission_mode: permissionMode.optional(), append_system_prompt: z.string().optional(),
    mcp_servers: z.record(z.string(), objectRecord).optional(), allowed_tools: z.array(z.string()).optional(),
    settings: objectRecord.optional(), env: z.record(z.string(), z.string()).optional(),
  }),
  z.strictObject({
    ...request, type: z.literal("prewarm"), executable: z.string().min(1), allowed_tools: z.array(z.string()).optional(),
    settings: objectRecord.optional(), env: z.record(z.string(), z.string()).optional(),
  }),
  z.strictObject({
    ...request, type: z.literal("claim"), cwd: z.string(), model: z.string().nullable().optional(),
    permission_mode: permissionMode.optional(), append_system_prompt: z.string().optional(), settings: objectRecord.optional(),
  }),
  z.strictObject({ ...request, type: z.literal("send"), request_id: id, content: z.array(objectRecord) }),
  z.strictObject({ ...request, type: z.literal("interrupt"), deadline_ms: z.number().int().positive().optional() }),
  z.strictObject({ ...request, type: z.literal("set_model"), model: z.string().nullable() }),
  z.strictObject({ ...request, type: z.literal("set_permission_mode"), mode: permissionMode }),
  z.strictObject({ ...request, type: z.literal("apply_flag_settings"), settings: objectRecord }),
  z.strictObject({ ...request, type: z.literal("set_mcp_servers"), servers: z.record(z.string(), objectRecord) }),
  z.strictObject({ ...request, type: z.literal("stop_task"), task_id: z.string() }),
  z.strictObject({ ...request, type: z.literal("answer"), interaction_id: id, status: z.enum(["answered", "cancelled"]), answer: objectRecord.optional() }),
  z.strictObject({ ...request, type: z.literal("close") }),
  z.strictObject({ ...request, type: z.literal("fork_session"), session_id: z.string(), dir: z.string().optional(), title: z.string().optional(), up_to_message_id: z.string().optional() }),
  z.strictObject({ ...request, type: z.literal("list_sessions"), dir: z.string().optional(), limit: z.number().int().min(1).max(200).optional(), offset: z.number().int().min(0).optional() }),
  z.strictObject({ ...request, type: z.literal("get_session_info"), session_id: z.string(), dir: z.string().optional() }),
  z.strictObject({ ...request, type: z.literal("get_session_messages"), session_id: z.string(), dir: z.string().optional(), limit: z.number().int().min(1).optional(), offset: z.number().int().min(0).optional() }),
  z.strictObject({ ...request, type: z.literal("supported_models"), executable: z.string().min(1) }),
]);
export type DaemonMessage = z.infer<typeof daemonMessageSchema>;
export type DaemonRequest<T extends DaemonMessage["type"]> = Extract<DaemonMessage, { type: T }>;

export type Dispatch = "rejected" | "not_dispatched" | "outcome_unknown";

export type AdapterMessage =
  | { version: 1; type: "ready"; sdk_version: string; cli_version?: string; capabilities?: string[] }
  | { version: 1; type: "result"; id: string; data?: unknown }
  | { version: 1; type: "error"; id?: string; code: string; message: string; dispatch?: Dispatch }
  | { version: 1; type: "sdk_message"; query_generation: number; message: { type: string } & Record<string, unknown> }
  | { version: 1; type: "interaction_request"; interaction_id: string; kind: "permission" | "question"; request: Record<string, unknown> };

/** An error the adapter reports with a stable code instead of free text. */
export class AdapterError extends Error {
  constructor(readonly code: string, message: string, readonly dispatch?: Dispatch) { super(message); }
}
