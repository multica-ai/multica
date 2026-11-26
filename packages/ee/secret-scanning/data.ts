import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { z } from "zod";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";
import { useSecretScanT } from "./i18n";

export type ScanMode = "notify" | "redact";
export type FindingStatus = "open" | "redacted" | "dismissed" | "resolved";
export type BackfillStatus = "idle" | "running" | "done" | "failed";

export interface ScanSettings {
  mode: ScanMode;
  backfill: {
    status: BackfillStatus;
    scanned: number;
    started_at: string | null;
    finished_at: string | null;
  };
}

/** A secret found in a comment. The secret itself never leaves the server. */
export interface Finding {
  id: string;
  issue_id: string;
  issue_identifier?: string;
  issue_title?: string;
  comment_id: string;
  author_type: string;
  author_id: string;
  rule: string;
  /** The secret with most of it hidden, e.g. ghp_••••••••0oQi. */
  masked: string;
  /** Same value for the same secret within a workspace. */
  fingerprint: string;
  status: FindingStatus;
  resolved_by_type: string | null;
  resolved_by_id: string | null;
  resolved_at: string | null;
  created_at: string;
}

export interface FindingCounts {
  open: number;
  redacted: number;
  dismissed: number;
}

export interface FindingList {
  findings: Finding[];
  counts: FindingCounts;
}

const DEFAULT_SETTINGS: ScanSettings = {
  mode: "notify",
  backfill: { status: "idle", scanned: 0, started_at: null, finished_at: null },
};

const SettingsSchema = z
  .object({
    mode: z.enum(["notify", "redact"]).catch("notify"),
    backfill: z
      .object({
        status: z.enum(["idle", "running", "done", "failed"]).catch("idle"),
        scanned: z.number().catch(0),
        started_at: z.string().nullable().catch(null),
        finished_at: z.string().nullable().catch(null),
      })
      .catch(DEFAULT_SETTINGS.backfill),
  })
  .loose();

const FindingSchema = z
  .object({
    id: z.string(),
    issue_id: z.string(),
    issue_identifier: z.string().optional().catch(undefined),
    issue_title: z.string().optional().catch(undefined),
    comment_id: z.string(),
    author_type: z.string(),
    author_id: z.string(),
    rule: z.string(),
    masked: z.string(),
    fingerprint: z.string(),
    status: z.enum(["open", "redacted", "dismissed", "resolved"]),
    resolved_by_type: z.string().nullable().catch(null),
    resolved_by_id: z.string().nullable().catch(null),
    resolved_at: z.string().nullable().catch(null),
    created_at: z.string(),
  })
  .loose();

/** A row the client cannot read is dropped, not the whole list. */
const findingArray = z
  .array(z.unknown())
  .default([])
  .catch([])
  .transform((rows) =>
    rows.flatMap((row) => {
      const parsed = FindingSchema.safeParse(row);
      return parsed.success ? [parsed.data] : [];
    }),
  );

const EMPTY_COUNTS: FindingCounts = { open: 0, redacted: 0, dismissed: 0 };

const ListSchema = z
  .object({
    findings: findingArray,
    counts: z
      .object({ open: z.number().catch(0), redacted: z.number().catch(0), dismissed: z.number().catch(0) })
      .catch(EMPTY_COUNTS),
  })
  .loose();

const IssueListSchema = z.object({ findings: findingArray }).loose();

const EMPTY_LIST: FindingList = { findings: [], counts: EMPTY_COUNTS };

const json = (method: string, body: unknown): RequestInit => ({ method, body: JSON.stringify(body) });

export const secretScanApi = {
  async settings(): Promise<ScanSettings> {
    return parseWithFallback(await api.request("/api/secret-scan/settings"), SettingsSchema, DEFAULT_SETTINGS, {
      endpoint: "GET /api/secret-scan/settings",
    });
  },
  async setMode(mode: ScanMode): Promise<ScanSettings> {
    return SettingsSchema.parse(await api.request("/api/secret-scan/settings", json("PUT", { mode })));
  },
  async backfill(): Promise<ScanSettings> {
    return SettingsSchema.parse(await api.request("/api/secret-scan/backfill", { method: "POST" }));
  },
  async findings(status: Exclude<FindingStatus, "resolved">): Promise<FindingList> {
    return parseWithFallback(await api.request(`/api/secret-scan/findings?status=${status}`), ListSchema, EMPTY_LIST, {
      endpoint: "GET /api/secret-scan/findings",
    });
  },
  async issueFindings(issueId: string): Promise<Finding[]> {
    const parsed = parseWithFallback(
      await api.request(`/api/secret-scan/issues/${issueId}/findings`),
      IssueListSchema,
      { findings: [] },
      { endpoint: "GET /api/secret-scan/issues/:id/findings" },
    );
    return parsed.findings;
  },
  async redact(findingIds: string[]): Promise<void> {
    await api.request("/api/secret-scan/findings/redact", json("POST", { finding_ids: findingIds }));
  },
  async dismiss(findingIds: string[]): Promise<void> {
    await api.request("/api/secret-scan/findings/dismiss", json("POST", { finding_ids: findingIds }));
  },
};

export const secretScanKeys = {
  all: (wsId: string) => ["ee", "secret-scan", wsId] as const,
  settings: (wsId: string) => [...secretScanKeys.all(wsId), "settings"] as const,
  findings: (wsId: string, status: string) => [...secretScanKeys.all(wsId), "findings", status] as const,
  issue: (wsId: string, issueId: string) => [...secretScanKeys.all(wsId), "issue", issueId] as const,
};

export function settingsOptions(wsId: string, enabled: boolean) {
  return queryOptions({
    queryKey: secretScanKeys.settings(wsId),
    queryFn: () => secretScanApi.settings(),
    enabled: enabled && wsId.length > 0,
    // Progress of a running scan is polled; its end also arrives over realtime.
    refetchInterval: (query) => (query.state.data?.backfill.status === "running" ? 2000 : false),
  });
}

export function findingsOptions(wsId: string, status: Exclude<FindingStatus, "resolved">, enabled: boolean) {
  return queryOptions({
    queryKey: secretScanKeys.findings(wsId, status),
    queryFn: () => secretScanApi.findings(status),
    enabled: enabled && wsId.length > 0,
  });
}

export function issueFindingsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: secretScanKeys.issue(wsId, issueId),
    queryFn: () => secretScanApi.issueFindings(issueId),
    enabled: wsId.length > 0 && issueId.length > 0,
    staleTime: 30_000,
  });
}

function errorText(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}

export function useSetMode(wsId: string) {
  const qc = useQueryClient();
  const t = useSecretScanT();
  return useMutation({
    mutationFn: (mode: ScanMode) => secretScanApi.setMode(mode),
    onSuccess: (settings) => qc.setQueryData(secretScanKeys.settings(wsId), settings),
    onError: (err) => toast.error(errorText(err, t("toast_save_failed"))),
  });
}

export function useStartBackfill(wsId: string) {
  const qc = useQueryClient();
  const t = useSecretScanT();
  return useMutation({
    mutationFn: () => secretScanApi.backfill(),
    onSuccess: (settings) => qc.setQueryData(secretScanKeys.settings(wsId), settings),
    onError: (err) => toast.error(errorText(err, t("toast_scan_failed"))),
    onSettled: () => qc.invalidateQueries({ queryKey: secretScanKeys.settings(wsId) }),
  });
}

/** Redacts or dismisses findings; every view of them refetches afterwards. */
export function useSettleFindings(wsId: string) {
  const qc = useQueryClient();
  const t = useSecretScanT();
  return useMutation({
    mutationFn: ({ action, ids }: { action: "redact" | "dismiss"; ids: string[] }) =>
      action === "redact" ? secretScanApi.redact(ids) : secretScanApi.dismiss(ids),
    onSuccess: (_data, { action }) => {
      if (action === "redact") toast.success(t("toast_redacted"));
    },
    onError: (err, { action }) =>
      toast.error(errorText(err, t(action === "redact" ? "toast_redact_failed" : "toast_dismiss_failed"))),
    onSettled: () => qc.invalidateQueries({ queryKey: secretScanKeys.all(wsId) }),
  });
}
