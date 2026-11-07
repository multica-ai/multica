import { z } from "zod";
import type { LocalConversationScope } from "@multica/core/local-conversation/protocol";
import { SETTINGS_OPERATIONS, type SettingsRequest, type SettingsScope } from "@multica/core/settings/operations";
import { developerLogQuerySchema, type DeveloperLogQuery } from "../shared/developer-logs";
import { SettingsOperationError } from "../shared/settings-operation-error";
import type { SettingsConfirmRequest, SettingsHostRequest, SettingsApplyOutcome } from "./settings-confirm";

export interface LocalV2HostHandlers {
  /** The signed-in account; requests for another account are refused. */
  currentUser(): string | null | undefined;
  side(request: unknown, signal: AbortSignal): Promise<unknown>;
  confirm(request: SettingsConfirmRequest, apply: SettingsRequest | null, signal: AbortSignal): Promise<SettingsApplyOutcome>;
  settingsHost(request: SettingsHostRequest, signal: AbortSignal): Promise<unknown>;
  developerLogs(scope: LocalConversationScope, query: DeveloperLogQuery, signal: AbortSignal): Promise<unknown>;
}

const daemonScope = z.object({ workspace_id: z.string(), user_id: z.string() });
const settingsPayload = z.object({ operation: z.string().regex(/^settings_[a-z_]+$/), scope: daemonScope, input: z.record(z.string(), z.unknown()), impact: z.unknown().optional(), source: z.string().max(200).optional() });

/**
 * The write a Settings operation performs, built here from the operation
 * and validated input. Host requests reach main through the
 * renderer, so a request the daemon sent along is never applied.
 */
export function settingsApply(operation: string, scope: SettingsScope, input: Record<string, unknown>): SettingsRequest | null {
  const op = SETTINGS_OPERATIONS.find((entry) => entry.name === operation);
  if (!op) throw new SettingsOperationError("unknown_operation", `Unknown settings operation ${operation}`);
  return op.request?.(scope, z.object(op.schema).parse(input)) ?? null;
}

const HOST_TIMEOUT_MS = 90_000;

/** Settings host operations that act on Desktop's window, not on the device. */
const VIEW_SETTINGS = new Set(["settings_open", "settings_assert_scope", "settings_cancel", "settings_capabilities", "settings_refresh"]);

/**
 * Host requests from another device's agents. Settings writes and
 * views happen here, with this Desktop's account, and Side control acts on
 * that device's page here; this device's own settings and logs are not for
 * another device.
 */
export function remoteHostRequests(handle: ReturnType<typeof localV2HostHandler>, deviceId: string, deviceName: () => Promise<string>): ReturnType<typeof localV2HostHandler> {
  return async (scope, kind, payload) => {
    if (kind === "settings.confirm") return handle(scope, kind, { ...payload, source: await deviceName().catch(() => deviceId) });
    if (kind === "settings.host" && !VIEW_SETTINGS.has(String(payload.operation))) throw new SettingsOperationError("device_settings_elsewhere", "This setting can only be changed on the device itself");
    if (kind === "developer_logs.export") throw new SettingsOperationError("unsupported_host_request", `Desktop cannot handle ${kind} for another device`);
    if (kind === "side.control") return handle(scope, kind, { ...payload, deviceId });
    return handle(scope, kind, payload);
  };
}

/** Sends a path the user chose to the daemon of a device; only main may. */
export function applyChosenPath(client: { request(scope: LocalConversationScope, method: string, path: string, body?: unknown, deviceId?: string): Promise<unknown> }, scope: LocalConversationScope, request: { purpose?: unknown; conversationId?: unknown }, path: string, deviceId: string): Promise<unknown> {
  if (request?.purpose === "preview") return client.request(scope, "POST", "/preview/roots", { path }, deviceId);
  if (request?.purpose === "folder_project") return client.request(scope, "POST", "/folder-projects", { path }, deviceId);
  if (request?.purpose === "conversation_directory" && typeof request.conversationId === "string") {
    return client.request(scope, "POST", `/sessions/${encodeURIComponent(request.conversationId)}/commands`, { type: "set_directory", cwd: path }, deviceId);
  }
  return Promise.reject(new Error("Invalid local conversation request"));
}

/**
 * Performs the daemon's v2 host requests, which the renderer receives on its
 * WebSocket and passes to main, with Desktop's existing handlers.
 * Every request must name the account and workspace of the socket it
 * arrived on, and that account must still be signed in.
 */
export function localV2HostHandler(handlers: LocalV2HostHandlers) {
  return async (scope: LocalConversationScope, kind: string, payload: Record<string, unknown>): Promise<unknown> => {
    if (handlers.currentUser() !== scope.userId) throw new SettingsOperationError("settings_host_unavailable", "Open the main app with the same signed-in account to continue");
    const signal = AbortSignal.timeout(HOST_TIMEOUT_MS);
    const settings = () => {
      const value = settingsPayload.parse(payload);
      if (value.scope.user_id !== scope.userId || value.scope.workspace_id !== scope.workspaceId) throw new SettingsOperationError("workspace_mismatch", "The request belongs to another account or workspace");
      return { ...value, scope: { userId: scope.userId, workspaceId: scope.workspaceId } };
    };
    switch (kind) {
      case "settings.confirm": {
        const request = settings();
        return handlers.confirm(request, settingsApply(request.operation, request.scope, request.input), signal);
      }
      case "settings.host": {
        const { operation, scope: requestScope, input } = settings();
        return handlers.settingsHost({ operation, scope: requestScope, input }, signal);
      }
      case "settings.refresh":
        return handlers.settingsHost({ operation: "settings_refresh", scope, input: {} }, signal);
      case "side.control": {
        const target = (payload.scope ?? {}) as { userId?: unknown; workspaceId?: unknown };
        if (target.userId !== scope.userId || target.workspaceId !== scope.workspaceId) throw new SettingsOperationError("workspace_mismatch", "The request belongs to another account or workspace");
        return handlers.side(payload, signal);
      }
      case "developer_logs.export":
        return handlers.developerLogs(scope, developerLogQuerySchema.parse(payload), signal);
    }
    throw new SettingsOperationError("unsupported_host_request", `Desktop cannot handle ${kind}`);
  };
}
