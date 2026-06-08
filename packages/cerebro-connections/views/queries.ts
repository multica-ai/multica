import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";

import type { Connection, CreateConnectionInput, UpdateConnectionInput } from "./types";

// ---------------------------------------------------------------------------
// Schemas — lenient per CLAUDE.md "API Response Compatibility"
// ---------------------------------------------------------------------------

const AuthConfigSchema = z
  .object({
    bearer_token: z.string().optional(),
    api_key: z.string().optional(),
    api_key_header: z.string().optional(),
    cf_access_id: z.string().optional(),
    cf_access_secret: z.string().optional(),
  })
  .loose();

const EndpointPermissionSchema = z
  .object({
    path: z.string(),
    methods: z.array(z.string()).default([]),
  })
  .loose();

const ConnectionSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    name: z.string(),
    display_name: z.string(),
    type: z.string(),
    url: z.string(),
    internal: z.boolean().default(false),
    auth_config: AuthConfigSchema.default({}),
    endpoint_permissions: z.array(EndpointPermissionSchema).default([]),
    enabled: z.boolean().default(true),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .loose();

const ConnectionListSchema = z
  .object({ connections: z.array(ConnectionSchema).default([]) })
  .loose();

const EMPTY_AUTH = {} as const;
const EMPTY_CONNECTION_STUB = (wsId: string): Connection => ({
  id: "",
  workspace_id: wsId,
  name: "",
  display_name: "",
  type: "mcp_http",
  url: "",
  internal: false,
  auth_config: EMPTY_AUTH,
  endpoint_permissions: [],
  enabled: true,
  created_at: "",
  updated_at: "",
});

// ---------------------------------------------------------------------------
// Query keys
// ---------------------------------------------------------------------------

export const connectionsKeys = {
  all: (wsId: string) => ["connections", wsId] as const,
};

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

export function useConnections(wsId: string) {
  return useQuery({
    queryKey: connectionsKeys.all(wsId),
    queryFn: async (): Promise<Connection[]> => {
      const raw = await api.listCerebroConnections(wsId);
      const parsed = parseWithFallback(
        raw,
        ConnectionListSchema,
        { connections: [] as Connection[] },
        { endpoint: "GET /api/workspaces/:id/connections" },
      );
      return parsed.connections as Connection[];
    },
    enabled: !!wsId,
  });
}

export function useCreateConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: CreateConnectionInput): Promise<Connection> => {
      const raw = await api.createCerebroConnection(wsId, input);
      return parseWithFallback(
        raw,
        ConnectionSchema,
        EMPTY_CONNECTION_STUB(wsId),
        { endpoint: "POST /api/workspaces/:id/connections" },
      ) as Connection;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: connectionsKeys.all(wsId) });
    },
  });
}

export function useUpdateConnection(wsId: string, connId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: UpdateConnectionInput): Promise<Connection> => {
      const raw = await api.updateCerebroConnection(wsId, connId, input);
      return parseWithFallback(
        raw,
        ConnectionSchema,
        EMPTY_CONNECTION_STUB(wsId),
        { endpoint: "PUT /api/workspaces/:id/connections/:connId" },
      ) as Connection;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: connectionsKeys.all(wsId) });
    },
  });
}

export function useDeleteConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (connId: string): Promise<void> => {
      await api.deleteCerebroConnection(wsId, connId);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: connectionsKeys.all(wsId) });
    },
  });
}

export function useToggleConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ conn, enabled }: { conn: Connection; enabled: boolean }): Promise<Connection> => {
      const input: UpdateConnectionInput = {
        display_name: conn.display_name,
        url: conn.url,
        internal: conn.internal,
        auth_config: conn.auth_config,
        endpoint_permissions: conn.endpoint_permissions,
        enabled,
      };
      const raw = await api.updateCerebroConnection(wsId, conn.id, input);
      return parseWithFallback(
        raw,
        ConnectionSchema,
        { ...conn, enabled },
        { endpoint: "PUT /api/workspaces/:id/connections/:connId" },
      ) as Connection;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: connectionsKeys.all(wsId) });
    },
  });
}
