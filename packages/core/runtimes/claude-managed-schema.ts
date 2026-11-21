import { z } from "zod";
import { parseWithFallback } from "../api/schema";
import { DEFAULT_CONCURRENCY, DEFAULT_RUN_TIMEOUT } from "./qoder-schema";

// Mirrors handler/claude_connection.go, whose defaults and bounds match Qoder's.
const schema = z
  .object({
    configured: z.boolean().catch(false),
    available: z.boolean().catch(false),
    name: z.string().catch(""),
    environment_id: z.string().catch(""),
    has_token: z.boolean().catch(false),
    repositories: z.array(z.string()).catch([]),
    vault_ids: z.array(z.string()).catch([]),
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
    environmentId: v.environment_id,
    hasToken: v.has_token,
    repositories: v.repositories,
    vaultIds: v.vault_ids,
    enabled: v.enabled,
    status: v.status,
    lastError: v.last_error,
    maxConcurrency: v.max_concurrency,
    runTimeoutMinutes: v.run_timeout_minutes,
    activeRuns: v.active_runs,
    queuedRuns: v.queued_runs,
  }));
export type ClaudeConnection = z.output<typeof schema>;
export type ClaudeInput = {
  name: string;
  environmentId: string;
  // Empty keeps the stored key; an omitted githubTokens keeps the stored repositories.
  anthropicToken: string;
  githubTokens?: Record<string, string>;
  vaultIds: string[];
  maxConcurrency: number;
  runTimeoutMinutes: number;
};
export function parseClaudeConnection(data: unknown): ClaudeConnection {
  return parseWithFallback(
    data,
    schema,
    {
      configured: false,
      available: false,
      name: "",
      environmentId: "",
      hasToken: false,
      repositories: [],
      vaultIds: [],
      enabled: false,
      status: "",
      lastError: "",
      maxConcurrency: DEFAULT_CONCURRENCY,
      runTimeoutMinutes: DEFAULT_RUN_TIMEOUT,
      activeRuns: 0,
      queuedRuns: 0,
    },
    { endpoint: "claude-managed" },
  );
}

const catalogSchema = z.object({
  items: z.array(z.object({ id: z.string().min(1), name: z.string() })),
});
export type ClaudeCatalogItem = z.output<typeof catalogSchema>["items"][number];
export function parseClaudeCatalog(data: unknown): ClaudeCatalogItem[] {
  return parseWithFallback(
    data,
    catalogSchema,
    { items: [] },
    { endpoint: "claude-managed/catalog" },
  ).items;
}
