import { z } from "zod";
import { parseWithFallback } from "../api/schema";

// Mirrors the server defaults and bounds in handler/qoder_connection.go.
export const DEFAULT_CONCURRENCY = 3;
export const MAX_CONCURRENCY = 10;
export const DEFAULT_RUN_TIMEOUT = 60;
export const MIN_RUN_TIMEOUT = 5;
export const MAX_RUN_TIMEOUT = 240;
// A PAT only works against the region that issued it.
export const QODER_REGIONS = {
  global: "https://api.qoder.com/api/v1/cloud",
  cn: "https://api.qoder.com.cn/api/v1/cloud",
} as const;
export type QoderRegion = keyof typeof QODER_REGIONS;

const schema = z
  .object({
    configured: z.boolean().catch(false),
    available: z.boolean().catch(false),
    name: z.string().catch(""),
    base_url: z.string().catch(""),
    environment_id: z.string().catch(""),
    has_token: z.boolean().catch(false),
    repositories: z.array(z.string()).catch([]),
    enabled: z.boolean().catch(false),
    status: z.string().catch(""),
    last_error: z.string().catch(""),
    max_concurrency: z.number().int().positive().catch(DEFAULT_CONCURRENCY),
    run_timeout_minutes: z.number().int().positive().catch(DEFAULT_RUN_TIMEOUT),
    active_runs: z.number().int().nonnegative().catch(0),
    queued_runs: z.number().int().nonnegative().catch(0),
  })
  .transform((v) => ({
    configured: v.configured,
    available: v.available,
    name: v.name,
    baseUrl: v.base_url,
    environmentId: v.environment_id,
    hasToken: v.has_token,
    repositories: v.repositories,
    enabled: v.enabled,
    status: v.status,
    lastError: v.last_error,
    maxConcurrency: v.max_concurrency,
    runTimeoutMinutes: v.run_timeout_minutes,
    activeRuns: v.active_runs,
    queuedRuns: v.queued_runs,
  }));
export type QoderConnection = z.output<typeof schema>;
export type QoderInput = {
  name: string;
  baseUrl: string;
  environmentId: string;
  qoderToken: string;
  // Omitted keeps the stored repository bindings, as an empty qoderToken
  // keeps the stored token.
  githubTokens?: Record<string, string>;
  maxConcurrency: number;
  runTimeoutMinutes: number;
};
export function parseQoderConnection(data: unknown): QoderConnection {
  return parseWithFallback(
    data,
    schema,
    {
      configured: false,
      available: false,
      name: "",
      baseUrl: "",
      environmentId: "",
      hasToken: false,
      repositories: [],
      enabled: false,
      status: "",
      lastError: "",
      maxConcurrency: DEFAULT_CONCURRENCY,
      runTimeoutMinutes: DEFAULT_RUN_TIMEOUT,
      activeRuns: 0,
      queuedRuns: 0,
    },
    { endpoint: "qoder" },
  );
}

const catalogSchema = z.object({
  items: z.array(z.object({ id: z.string().min(1), name: z.string() })),
});
export type QoderCatalogItem = z.output<typeof catalogSchema>["items"][number];
export function parseQoderCatalog(data: unknown): QoderCatalogItem[] {
  return parseWithFallback(
    data,
    catalogSchema,
    { items: [] },
    { endpoint: "qoder/catalog" },
  ).items;
}
