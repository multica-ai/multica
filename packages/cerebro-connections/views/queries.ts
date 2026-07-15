import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";

import type {
  Connection,
  CreateConnectionInput,
  UpdateConnectionInput,
  ScopableArgOption,
} from "./types";

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
    session_exchange: z
      .object({
        enabled: z.boolean().default(false),
        path: z.string().optional(),
        ttl_seconds: z.number().optional(),
      })
      .loose()
      .optional(),
  })
  .loose();

const EndpointPermissionSchema = z
  .object({
    path: z.string(),
    methods: z.array(z.string()).default([]),
    summary: z.string().optional(),
    granted: z.boolean().optional(),
  })
  .loose();

const ScopableArgSchema = z
  .object({
    tool: z.string(),
    arg: z.string(),
    options_source_tool: z.string(),
    group_by: z.string().optional(),
    tag_field: z.string().optional(),
    label: z.string().optional(),
  })
  .loose();

export const ConnectionSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string(),
    name: z.string(),
    display_name: z.string(),
    instructions: z.string().default(""),
    type: z.string(),
    url: z.string(),
    internal: z.boolean().default(false),
    auth_config: AuthConfigSchema.default({}),
    endpoint_permissions: z.array(EndpointPermissionSchema).default([]),
    scopable_args: z.array(ScopableArgSchema).default([]),
    // Unknown/missing default_access downgrades to the safe "deny" rather than
    // throwing (enum-drift rule); the server is the source of truth.
    default_access: z.enum(["allow", "ask", "deny"]).catch("deny").default("deny"),
    tools: z
      .array(z.object({ name: z.string(), description: z.string().optional() }).loose())
      .optional(),
    enabled: z.boolean().default(true),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .loose();

const ScopableArgOptionSchema = z
  .object({
    id: z.string(),
    name: z.string().default(""),
    folder: z.string().optional(),
    folder_id: z.string().optional(),
    tags: z.array(z.string()).optional(),
  })
  .loose();

const ConnectionOptionsSchema = z
  .object({ options: z.array(ScopableArgOptionSchema).default([]) })
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
  instructions: "",
  type: "mcp_http",
  url: "",
  internal: false,
  auth_config: EMPTY_AUTH,
  endpoint_permissions: [],
  scopable_args: [],
  default_access: "deny",
  enabled: true,
  created_at: "",
  updated_at: "",
});

// ---------------------------------------------------------------------------
// Schemas — test result
// ---------------------------------------------------------------------------

const ToolInfoSchema = z.object({ name: z.string(), description: z.string().optional() }).loose();

const DiscoveredEndpointSchema = z
  .object({
    path: z.string(),
    methods: z.array(z.string()).default([]),
    summary: z.string().optional(),
    granted: z.boolean().optional(),
  })
  .loose();

export const TestResultSchema = z
  .object({
    reachable: z.boolean(),
    status_code: z.number().optional(),
    tools: z.array(ToolInfoSchema).optional(),
    endpoints: z.array(DiscoveredEndpointSchema).optional(),
    error: z.string().optional(),
  })
  .loose();

export type TestResult = z.infer<typeof TestResultSchema>;

// ---------------------------------------------------------------------------
// Query keys
// ---------------------------------------------------------------------------

export const connectionsKeys = {
  all: (wsId: string) => ["connections", wsId] as const,
  one: (wsId: string, connId: string) => ["connections", wsId, connId] as const,
  options: (wsId: string, connId: string, tool: string) =>
    ["connections", wsId, connId, "options", tool] as const,
};

// ---------------------------------------------------------------------------
// Hooks
// ---------------------------------------------------------------------------

export function useConnection(wsId: string, connId: string) {
  return useQuery({
    queryKey: connectionsKeys.one(wsId, connId),
    queryFn: async (): Promise<Connection> => {
      const raw = await api.getCerebroConnection(wsId, connId);
      return parseWithFallback(
        raw,
        ConnectionSchema,
        EMPTY_CONNECTION_STUB(wsId),
        { endpoint: "GET /api/workspaces/:id/connections/:connId" },
      ) as Connection;
    },
    enabled: !!wsId && !!connId,
  });
}

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

// useConnectionOptions fetches the choices for a scopable arg's picker by
// calling the connection's declared options-source tool (FIR-2083). Enabled only
// when a tool is given, so the When popover can lazily fetch when opened.
export function useConnectionOptions(
  wsId: string,
  connId: string,
  tool: string,
  enabled = true,
) {
  return useQuery({
    queryKey: connectionsKeys.options(wsId, connId, tool),
    queryFn: async (): Promise<ScopableArgOption[]> => {
      const raw = await api.getCerebroConnectionOptions(wsId, connId, tool);
      const parsed = parseWithFallback(
        raw,
        ConnectionOptionsSchema,
        { options: [] as ScopableArgOption[] },
        { endpoint: "GET /api/workspaces/:id/connections/:connId/options" },
      );
      return parsed.options as ScopableArgOption[];
    },
    enabled: !!wsId && !!connId && !!tool && enabled,
    staleTime: 60_000,
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
      // The edit page stays open after saving (FIR-2640), so refresh the
      // single-connection cache it renders from, not just the list.
      void qc.invalidateQueries({ queryKey: connectionsKeys.one(wsId, connId) });
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

export function useTestConnection(wsId: string) {
  return useMutation({
    mutationFn: async (input: {
      url: string;
      type: string;
      auth_config: Connection["auth_config"];
      connection_id?: string;
      // Explicit OpenAPI spec source (api type): a direct document URL, or the
      // raw text of an uploaded document. spec_content wins over spec_url.
      spec_url?: string;
      spec_content?: string;
    }): Promise<TestResult> => {
      const raw = await api.testCerebroConnection(wsId, input);
      return parseWithFallback(
        raw,
        TestResultSchema,
        { reachable: false, error: "unexpected response" },
        { endpoint: "POST /api/workspaces/:id/connections/test" },
      ) as TestResult;
    },
  });
}

export function useToggleConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async ({ conn, enabled }: { conn: Connection; enabled: boolean }): Promise<Connection> => {
      const input: UpdateConnectionInput = {
        display_name: conn.display_name,
        instructions: conn.instructions,
        url: conn.url,
        internal: conn.internal,
        auth_config: conn.auth_config,
        endpoint_permissions: conn.endpoint_permissions,
        scopable_args: conn.scopable_args,
        enabled,
        default_access: conn.default_access,
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
