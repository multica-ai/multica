import { z } from "zod";
import type { ApiClient } from "../api/client";
import type { IssueStatus } from "../types";

/** Jira ADF (Atlassian Document Format) is a recursive node tree. We keep the
 *  schema permissive — adf.ts walks it for text, and we never re-serialize it,
 *  so unknown node types degrade gracefully. */
export const JiraCommentSchema = z.object({
  id: z.string(),
  created: z.string(),
  body: z.unknown().optional(),
  author: z.object({ displayName: z.string().optional() }).partial().optional(),
});
export type JiraComment = z.infer<typeof JiraCommentSchema>;

export const JiraIssueFieldsSchema = z.object({
  summary: z.string().default(""),
  description: z.unknown().nullable().default(null),
  duedate: z.string().nullable().default(null),
  updated: z.string().default(""),
  // statusCategory.key is language-independent ("new" | "indeterminate" |
  // "done"), unlike the localized status.name — used as the mapping fallback
  // so non-English Jira instances still land in the right Multica status.
  status: z
    .object({
      name: z.string().default(""),
      statusCategory: z.object({ key: z.string().default("") }).default({ key: "" }),
    })
    .default({ name: "", statusCategory: { key: "" } }),
  priority: z.object({ name: z.string() }).nullable().default(null),
  subtasks: z.array(z.object({ key: z.string() })).default([]),
  comment: z
    .object({ comments: z.array(JiraCommentSchema).default([]) })
    .default({ comments: [] }),
});

export const JiraIssueSchema = z.object({
  key: z.string(),
  fields: JiraIssueFieldsSchema,
});
export type JiraIssue = z.infer<typeof JiraIssueSchema>;

export const JiraSearchResponseSchema = z.object({
  issues: z.array(JiraIssueSchema).default([]),
  total: z.number().default(0),
});
export type JiraSearchResponse = z.infer<typeof JiraSearchResponseSchema>;

const EMPTY_SEARCH: JiraSearchResponse = { issues: [], total: 0 };

/** Parse a Jira /search response, degrading to an empty result on any shape
 *  mismatch — mirrors the repo's parseWithFallback discipline for network JSON. */
export function parseJiraSearch(raw: unknown): JiraSearchResponse {
  const parsed = JiraSearchResponseSchema.safeParse(raw);
  return parsed.success ? parsed.data : EMPTY_SEARCH;
}

/** Single-issue fetch (used for subtasks), same fallback discipline. */
export function parseJiraIssue(raw: unknown): JiraIssue | null {
  const parsed = JiraIssueSchema.safeParse(raw);
  return parsed.success ? parsed.data : null;
}

/** Transport injected into the sync engine. In the desktop app this is backed
 *  by the main-process `jira:request` IPC channel; tests pass a fake. */
export type JiraTransport = (req: {
  method: string;
  path: string;
  body?: unknown;
}) => Promise<unknown>;

export interface JiraConfig {
  /** e.g. https://acme.atlassian.net (no trailing slash) */
  siteUrl: string;
  email: string;
  /** JQL filter; default targets the token owner. */
  jql: string;
  /** Override map: lowercased Jira status name -> Multica status. */
  statusMapping: Record<string, IssueStatus>;
  /** 0 disables auto-polling. */
  pollIntervalMinutes: number;
}

export interface SyncResult {
  created: number;
  updated: number;
  skipped: number;
  commentsAdded: number;
  errors: { jiraKey: string; message: string }[];
}

export interface SyncDeps {
  transport: JiraTransport;
  api: ApiClient;
  config: JiraConfig;
  /** Multica member the synced issues are created/assigned as. */
  currentMemberId: string;
}
