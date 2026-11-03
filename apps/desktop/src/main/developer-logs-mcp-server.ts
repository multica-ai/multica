import { randomUUID } from "node:crypto";
import { readFile, rename, rm, writeFile, mkdir } from "node:fs/promises";
import { join } from "node:path";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import { buildDeveloperLogBundle, writeDeveloperLogExport, type RuntimeInfo } from "./developer-log-export";
import { WebDiagnosticLog, snapshotDaemonLog, type RuntimeLogSource } from "./developer-runtime-logs";
import { LocalTimingLog, snapshotDaemonTiming, snapshotLinkTiming } from "./local-timing-log";

export const DEVELOPER_LOGS_MCP_NAME = "multica_developer_logs";
const MANIFEST_FILE = "developer-logs-mcp.json";

/** What the desktop tells the standalone server; contains paths and IDs, never credentials. */
export interface DeveloperLogsManifest {
  version: 1;
  apiUrl: string;
  userId: string | null;
  daemonLogPath: string | null;
  /** Absent from a manifest an older Desktop published; that source then reads as unavailable. */
  daemonTimingPath?: string | null;
  runtime: RuntimeInfo;
}
const manifestSchema = z.object({
  version: z.literal(1),
  apiUrl: z.string(),
  userId: z.string().min(1).max(128).nullable(),
  daemonLogPath: z.string().nullable(),
  daemonTimingPath: z.string().nullable().optional(),
  runtime: z.object({ appVersion: z.string(), platform: z.string(), arch: z.string(), electronVersion: z.string(), chromeVersion: z.string(), nodeVersion: z.string(), packaged: z.boolean() }),
});

export async function writeDeveloperLogsManifest(diagnosticsDir: string, manifest: DeveloperLogsManifest): Promise<void> {
  await mkdir(diagnosticsDir, { recursive: true, mode: 0o700 });
  const path = join(diagnosticsDir, MANIFEST_FILE);
  const temporary = `${path}.${randomUUID()}.tmp`;
  try {
    await writeFile(temporary, JSON.stringify(manifest), { mode: 0o600, flag: "wx" });
    await rename(temporary, path);
  } finally { await rm(temporary, { force: true }).catch(() => undefined); }
}

async function readManifest(diagnosticsDir: string): Promise<DeveloperLogsManifest | null> {
  try { return manifestSchema.parse(JSON.parse(await readFile(join(diagnosticsDir, MANIFEST_FILE), "utf8"))); }
  catch { return null; }
}

const unavailableWeb: RuntimeLogSource = { status: "unavailable", truncated: false, incomplete: false, maxBytes: 0, records: [] };

/**
 * Device-wide developer log export for agents that run outside desktop
 * conversations (daemon tasks, terminal Codex). It only reads the files the
 * desktop already writes; it cannot reach conversations, tasks or the cloud.
 */
export function createDeveloperLogsMcpServer(diagnosticsDir: string): McpServer {
  const mcp = new McpServer({ name: DEVELOPER_LOGS_MCP_NAME, version: "1.0.0" }, {
    instructions: "Exports Multica Desktop developer diagnostics from this computer. Timings cover every local conversation in the time range, not a single workspace. Use only when the user is diagnosing or reporting a problem.",
  });
  const exportsRoot = join(diagnosticsDir, "exports");
  mcp.registerTool("export_developer_logs", {
    description: "Export Multica Desktop developer diagnostics from this computer into one private JSON file: local conversation stage timings and per-turn waterfalls from the desktop and daemon (all conversations on this device), the signed-in account's Web renderer console for the same time range, runtime versions and the desktop daemon log tail (not time-filtered). since_minutes defaults to 60. Returns the local path, per-source availability and counts; copy the file into your working directory before attaching it. Common credentials, URLs and payload fields are scrubbed, but diagnostic text may contain private details: never upload it without the user's request. Exports are kept up to 7 days, at most 20 files.",
    inputSchema: { since_minutes: z.number().int().min(1).max(10080).default(60) },
    annotations: { readOnlyHint: false, destructiveHint: false, idempotentHint: false, openWorldHint: false },
  }, async ({ since_minutes }) => {
    try {
      const manifest = await readManifest(diagnosticsDir);
      if (!manifest) throw new Error("Multica Desktop has not published its diagnostics on this computer. Open Multica Desktop and try again.");
      const until = Date.now();
      const since = until - since_minutes * 60_000;
      const [timing, daemonTiming, linkTiming] = await Promise.all([
        new LocalTimingLog(diagnosticsDir).snapshot(null, since, until),
        snapshotDaemonTiming(manifest.daemonTimingPath ?? null, null, since, until),
        snapshotLinkTiming(manifest.daemonTimingPath ?? null, since, until),
      ]);
      const [web, daemon] = await Promise.all([
        manifest.userId ? new WebDiagnosticLog(diagnosticsDir, manifest.apiUrl, manifest.userId).snapshot(since, until) : unavailableWeb,
        snapshotDaemonLog(manifest.daemonLogPath),
      ]);
      const bundle = buildDeveloperLogBundle({ runtime: manifest.runtime, workspaceId: null, since, until, conversationId: null, timing, daemonTiming, linkTiming, sources: { web, daemon }, timingScope: "device" });
      const result = await writeDeveloperLogExport(exportsRoot, bundle);
      return { content: [{ type: "text", text: JSON.stringify(result) }] };
    } catch (error) {
      return { isError: true, content: [{ type: "text", text: error instanceof Error && error.message.startsWith("Multica Desktop") ? error.message : "Could not export developer logs" }] };
    }
  });
  return mcp;
}
