import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { api, parseWithFallback } from "@multica/core/api";
import type { SessionMode } from "./modes";

export type ThinkingLevel = "low" | "medium" | "high" | "xhigh";
export type ApprovalPolicy = "inherit" | "require" | "deny_external";

export interface ModeConfig {
  mode: SessionMode;
  version: string;
  instruction: string;
  model: string;
  thinking_level: ThinkingLevel;
  timeout_minutes: number;
  max_turns: number;
  allows_write: boolean;
  allowed_tools: string[];
  data_sources: string[];
  approval_policy: ApprovalPolicy;
  eval_ids: string[];
}

export interface ModeVersion {
  id: string;
  version: string;
  config: ModeConfig;
  description: string;
  created_by: string;
  created_at: string;
}

export interface ModeRecord {
  mode: SessionMode;
  active_version: string;
  active: ModeConfig;
  draft: ModeConfig | null;
  versions: ModeVersion[];
  updated_at: string;
}

const modeSchema = z.enum(["plan", "build", "research", "review"]);
const nullableStringListSchema = z.array(z.string()).nullish().transform((value) => value ?? []);
export const modeConfigSchema = z.object({
  mode: modeSchema.default("build"),
  version: z.string().default(""),
  instruction: z.string().default(""),
  model: z.string().default(""),
  thinking_level: z.enum(["low", "medium", "high", "xhigh"]).default("medium"),
  timeout_minutes: z.number().default(30),
  max_turns: z.number().default(20),
  allows_write: z.boolean().default(false),
  allowed_tools: nullableStringListSchema,
  data_sources: nullableStringListSchema,
  approval_policy: z.enum(["inherit", "require", "deny_external"]).default("inherit"),
  eval_ids: nullableStringListSchema,
}).loose();

const modeVersionSchema = z.object({
  id: z.string().default(""),
  version: z.string().default(""),
  config: modeConfigSchema,
  description: z.string().default(""),
  created_by: z.string().default(""),
  created_at: z.string().default(""),
}).loose();

export const modeRecordSchema = z.object({
  mode: modeSchema,
  active_version: z.string().default("1"),
  active: modeConfigSchema,
  draft: modeConfigSchema.nullish().default(null),
  versions: z.array(modeVersionSchema).default([]),
  updated_at: z.string().default(""),
}).loose();

export const modeConfigListSchema = z.object({
  modes: z.array(modeRecordSchema).default([]),
}).loose();

export const EMPTY_MODE_CONFIG_LIST: { modes: ModeRecord[] } = { modes: [] };
const base = "/api/cerebro/session-modes";

export async function listModeConfigs(): Promise<{ modes: ModeRecord[] }> {
  const raw = await api.cerebroRequest<unknown>(`${base}/`);
  return parseWithFallback(raw, modeConfigListSchema, EMPTY_MODE_CONFIG_LIST, {
    endpoint: `${base}/`,
  }) as { modes: ModeRecord[] };
}

async function mutateRecord(path: string, method: "PUT" | "POST", body: unknown): Promise<ModeRecord> {
  const raw = await api.cerebroRequest<unknown>(path, { method, body: JSON.stringify(body) });
  const parsed = modeRecordSchema.safeParse(raw);
  if (!parsed.success) throw new Error("The Mode response was invalid");
  return parsed.data as ModeRecord;
}

export const modeConfigKeys = {
  all: (workspaceId: string) => ["cerebro", "session-modes", workspaceId] as const,
};

export function modeConfigsOptions(workspaceId: string) {
  return queryOptions({
    queryKey: modeConfigKeys.all(workspaceId),
    queryFn: listModeConfigs,
    enabled: !!workspaceId,
    staleTime: 15_000,
  });
}

export function useSaveModeDraft(workspaceId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ mode, config }: { mode: SessionMode; config: ModeConfig }) =>
      mutateRecord(`${base}/${mode}/draft`, "PUT", config),
    onSuccess: () => client.invalidateQueries({ queryKey: modeConfigKeys.all(workspaceId) }),
  });
}

export function usePublishMode(workspaceId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ mode, description }: { mode: SessionMode; description: string }) =>
      mutateRecord(`${base}/${mode}/publish`, "POST", { description }),
    onSuccess: () => client.invalidateQueries({ queryKey: modeConfigKeys.all(workspaceId) }),
  });
}

export function useRestoreMode(workspaceId: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ mode, version }: { mode: SessionMode; version: string }) =>
      mutateRecord(`${base}/${mode}/restore`, "POST", { version }),
    onSuccess: () => client.invalidateQueries({ queryKey: modeConfigKeys.all(workspaceId) }),
  });
}
