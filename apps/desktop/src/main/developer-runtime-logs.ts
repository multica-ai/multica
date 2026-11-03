import { createHash } from "node:crypto";
import { appendFile, mkdir, open, rename, stat } from "node:fs/promises";
import { join } from "node:path";
import { stripVTControlCharacters } from "node:util";
import { LOCAL_TIMING_PREFIX } from "@multica/tutti-agent-gui-host/local-timing";

const WEB_FILE_BYTES = 2 * 1024 * 1024;
const DAEMON_TAIL_BYTES = 2 * 1024 * 1024;
const MAX_MESSAGE_CHARS = 16 * 1024;
const levels = new Set(["debug", "info", "warning", "error"]);

export interface WebLogRecord {
  at: string;
  level: string;
  message: string;
  source?: string;
  line?: number;
}
export interface RuntimeLogSource {
  status: "available" | "empty" | "missing" | "unavailable" | "unreadable";
  truncated: boolean;
  incomplete: boolean;
  maxBytes: number;
  records: WebLogRecord[];
}
export interface DaemonLogSource extends Omit<RuntimeLogSource, "records"> {
  scope: "desktop-profile";
  timeFilterApplied: false;
  modifiedAt?: string;
  text: string;
}

const PEER_ID = /\b(?:12D3Koo[1-9A-HJ-NP-Za-km-z]{20,}|Qm[1-9A-HJ-NP-Za-km-z]{44})\b/g;

/** Peer IDs stay readable until a bundle exists, which owns the key that makes them comparable within it. */
export function hashPeerIds(value: string, hash: (id: string) => string): string {
  return value.replace(PEER_ID, (id) => `peer:${hash(id)}`);
}

/** Best-effort scrubbing, not a guarantee that arbitrary application text is private. */
export function redactDiagnosticText(value: string): string {
  return stripVTControlCharacters(value)
    .replace(/-----BEGIN[^\r\n]*PRIVATE KEY-----[\s\S]*/g, "[REDACTED PRIVATE KEY]")
    .replace(/^([A-Za-z0-9+/]{40,}={0,2})$/gm, "[REDACTED ENCODED DATA]")
    .replace(/data:[^\s;,]+(?:;[^,\s]*)?,[^\s"'<>)]*/gi, "[REDACTED DATA]")
    .replace(/\b(?:Bearer|Basic)\s+[^\s"',;]+/gi, "[REDACTED AUTH]")
    .replace(/\b(?:mul_[\w-]+|(?:sk-|gh[pousr]_|github_pat_|glpat-|xox[baprs]-|xapp-)[\w-]{10,}|AKIA[A-Z0-9]{16}|eyJ[\w-]+\.[\w-]+\.[\w-]+)\b/g, "[REDACTED TOKEN]")
    .replace(/(\b[a-z][a-z\d+.-]*:\/\/)[^\s/@]+@/gi, "$1[REDACTED]@")
    // libp2p multiaddrs, in daemon logs and in the raw errors it quotes.
    .replace(/(?<![\w./-])\/(?:ip4|ip6|dns|dns4|dns6|dnsaddr)\/[^\s"',;()[\]]+/g, "[REDACTED ADDR]")
    // Query and fragment values can carry signed URLs, login codes or search text.
    .replace(/([?&#][\w.%~-]+=)[^\s&#"'<>)]*/g, "$1[REDACTED]")
    .replace(/(\b(?:set-cookie|cookie)\s*:\s*)[^\r\n]*/gi, "$1[REDACTED]")
    .replace(/(["']?\b(?:[\w.-]*(?:token|secret|password|api[_-]?key|private[_-]?key)|authorization|cookie|set-cookie|session[_-]?id)["']?\s*[:=]\s*)(?:"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'|[^\r\n,;]+)/gi, "$1[REDACTED]")
    // Diagnostic payload fields are not needed to diagnose transport/lifecycle issues.
    .replace(/(["']?\b(?:prompt|messages?|content|body|input|output|env|headers|arguments)["']?\s*[:=]\s*)[^\r\n]*/gi, "$1[REDACTED PAYLOAD]");
}

function sanitizeMessage(value: string): string {
  // Oversized input is dropped as a whole: truncating first could expose a secret's tail.
  if (value.length > MAX_MESSAGE_CHARS) return "[OVERSIZED LOG MESSAGE OMITTED]";
  return redactDiagnosticText(value);
}

function parseWebRecord(value: unknown): WebLogRecord | undefined {
  if (!value || typeof value !== "object") return;
  const row = value as Record<string, unknown>;
  if (typeof row.at !== "string" || !Number.isFinite(Date.parse(row.at)) || typeof row.message !== "string" || typeof row.level !== "string" || !levels.has(row.level)) return;
  return {
    at: new Date(row.at).toISOString(), level: row.level, message: sanitizeMessage(row.message),
    ...(typeof row.source === "string" ? { source: sanitizeMessage(row.source) } : {}),
    ...(typeof row.line === "number" && Number.isInteger(row.line) && row.line >= 0 ? { line: row.line } : {}),
  };
}

function isMissing(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && "code" in error && error.code === "ENOENT");
}

/** One account's renderer console, bounded and serialized independently of conversation work. */
export class WebDiagnosticLog {
  private pending = Promise.resolve();
  private queued = 0;
  private size?: number;
  private dropped = false;
  readonly path: string;
  constructor(private directory: string, apiUrl: string, userId: string, private maxBytes = WEB_FILE_BYTES) {
    const key = createHash("sha256").update(JSON.stringify([apiUrl, userId])).digest("hex");
    this.path = join(directory, `web-${key}.jsonl`);
  }

  console(details: { level: string; message: string; sourceId?: string; lineNumber?: number }): void {
    if (details.message.startsWith(LOCAL_TIMING_PREFIX)) return;
    if (this.queued >= 1000) { this.dropped = true; return; }
    const record = parseWebRecord({ at: new Date().toISOString(), level: levels.has(details.level) ? details.level : "info", message: details.message, source: details.sourceId, line: details.lineNumber });
    if (!record) return;
    const line = JSON.stringify(record) + "\n";
    if (Buffer.byteLength(line) > this.maxBytes) { this.dropped = true; return; }
    this.queued++;
    this.pending = this.pending.then(async () => {
      if (this.size === undefined) {
        await mkdir(this.directory, { recursive: true, mode: 0o700 });
        this.size = await stat(this.path).then((file) => file.size, () => 0);
      }
      if (this.size > 0 && this.size + Buffer.byteLength(line) > this.maxBytes) {
        await rename(this.path, this.path + ".1"); this.size = 0;
      }
      await appendFile(this.path, line, { mode: 0o600 });
      this.size += Buffer.byteLength(line);
    }).catch(() => { this.dropped = true; this.size = undefined; }).finally(() => { this.queued--; });
  }

  snapshot(since: number, until: number): Promise<RuntimeLogSource> {
    const snapshot = this.pending.then(async () => {
      const records: WebLogRecord[] = [];
      let incomplete = this.dropped;
      let present = false;
      let truncated = false;
      for (const path of [this.path + ".1", this.path]) {
        try {
          const file = await open(path, "r");
          try {
            present = true;
            if (path.endsWith(".1")) truncated = true;
            if ((await file.stat()).size > this.maxBytes) { incomplete = true; continue; }
            const bytes = Buffer.alloc(this.maxBytes);
            const { bytesRead } = await file.read(bytes, 0, bytes.length, 0);
            for (const line of bytes.subarray(0, bytesRead).toString("utf8").split("\n")) {
              if (!line.trim()) continue;
              try {
                const record = parseWebRecord(JSON.parse(line));
                if (!record) { incomplete = true; continue; }
                const at = Date.parse(record.at);
                if (at >= since && at <= until) records.push(record);
              } catch { incomplete = true; }
            }
          } finally { await file.close(); }
        } catch (error) { if (!isMissing(error)) incomplete = true; }
      }
      records.sort((a, b) => a.at.localeCompare(b.at));
      return { status: incomplete && !present ? "unreadable" : records.length ? "available" : present ? "empty" : "missing", truncated, incomplete, maxBytes: this.maxBytes, records } satisfies RuntimeLogSource;
    });
    this.pending = snapshot.then(() => undefined, () => undefined);
    return snapshot;
  }

  flush(): Promise<void> { return this.pending; }
}

/** Only callers in main resolve this path; renderer and MCP input cannot select files. */
export async function snapshotDaemonLog(path: string | null, maxBytes = DAEMON_TAIL_BYTES): Promise<DaemonLogSource> {
  const source: DaemonLogSource = { status: "unavailable", scope: "desktop-profile", timeFilterApplied: false, truncated: false, incomplete: false, maxBytes, text: "" };
  if (!path) return source;
  try {
    const file = await open(path, "r");
    try {
      const before = await file.stat();
      if (!before.isFile()) return { ...source, status: "unreadable", incomplete: true };
      const length = Math.min(before.size, maxBytes);
      const bytes = Buffer.alloc(length);
      const { bytesRead } = await file.read(bytes, 0, length, before.size - length);
      let text = bytes.subarray(0, bytesRead).toString("utf8");
      // A tail may start in the middle of a credential, multibyte character or line.
      if (before.size > length) {
        const firstNewline = text.indexOf("\n");
        text = firstNewline < 0 ? "" : text.slice(firstNewline + 1);
      }
      // An append still in progress is not a complete diagnostic record.
      const complete = text.endsWith("\n") || !text;
      if (!complete) text = text.slice(0, Math.max(0, text.lastIndexOf("\n") + 1));
      const after = await file.stat();
      text = redactDiagnosticText(text).split("\n").map(sanitizeMessage).join("\n");
      return { ...source, status: text ? "available" : "empty", modifiedAt: before.mtime.toISOString(), truncated: before.size > length, incomplete: !complete || bytesRead < length || after.size < before.size, text };
    } finally { await file.close(); }
  } catch (error) { return { ...source, status: isMissing(error) ? "missing" : "unreadable", incomplete: !isMissing(error) }; }
}
