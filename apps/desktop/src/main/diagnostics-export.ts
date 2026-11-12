import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  type SaveDialogOptions,
} from "electron";
import { constants } from "fs";
import { lstat, mkdtemp, open, realpath, rename, rm } from "fs/promises";
import { dirname, join, resolve } from "path";
import { writeZip } from "@multica/core/appearance/theme-archive";
import { redactSecrets } from "@multica/core/security/redact-secrets";
import { redactText } from "@multica/core/analytics/redact-exception";
import type { DaemonState } from "../shared/daemon-types";
import {
  DIAGNOSTICS_EXPORT_CHANNEL,
  type DiagnosticsExportResult,
} from "../shared/diagnostics";
import { profileDir, profileLogPath } from "./daemon-profile";

const MAX_LOG_BYTES = 32 * 1024;
const MAX_LOG_LINES = 200;
// The daemon rotates at 20 MiB. Scan a bounded snapshot from its lexical start,
// keeping only one bounded logical record and the redacted output in memory.
const MAX_LOG_SOURCE_BYTES = 24 * 1024 * 1024;
const LOG_READ_CHUNK_BYTES = 64 * 1024;
const MAX_LOG_RECORD_BYTES = 256 * 1024;
const MAX_LOG_READ_MS = 10_000;
const MAX_METADATA_BYTES = 16 * 1024;
const MAX_ARCHIVE_BYTES = 64 * 1024;
const LOG_RECORD =
  /^\d{2}:\d{2}:\d{2}\.\d{3}\s+(?:DEBUG|DBG|INFO|INF|WARN|WRN|ERROR|ERR)(?:[+-]\d+)?\s/;

interface DiagnosticsSnapshot {
  profile: string | null;
  state: DaemonState;
  cliVersion: string | null;
}

interface LogTail {
  status: "available" | "unresolved" | "unavailable";
  truncated: boolean;
  omittedRecords: number;
  omissionReason?: "source_limit" | "incomplete_value" | "record_limit" | "read_limit";
  text: string;
}

function redactLog(
  text: string,
  depth = 0,
): { text: string; incompleteValue: boolean } {
  let incompleteValue = false;
  // tint quotes string/error attributes with strconv.AppendQuote. Decode each
  // quoted span before redaction, then re-encode it to preserve record boundaries.
  // Nested JSON strings have a fixed depth budget. Unsupported Go escapes and
  // incomplete strings are withheld instead of exporting their undecoded bytes.
  // Complete spans include raw newlines so malformed values cannot expose fake
  // records. An unterminated span is withheld through the end of the snapshot.
  // A timestamp-shaped continuation cannot establish a safe record boundary.
  const decoded = text.replace(
    /"(?:[^"\\]|\\[\s\S])*"|("(?:[^"\\]|\\[\s\S])*\\?$)/g,
    (quoted, incomplete: string | undefined) => {
      if (incomplete !== undefined) {
        incompleteValue = true;
        return '"[REDACTED INCOMPLETE VALUE]"';
      }
      if (depth >= 4) return '"[REDACTED ENCODED VALUE]"';
      try {
        return JSON.stringify(
          redactLog(JSON.parse(quoted) as string, depth + 1).text,
        );
      } catch {
        return '"[REDACTED ENCODED VALUE]"';
      }
    },
  );
  // Quoted values may span lines. Redact before selecting individual records.
  let safe = decoded
    .replace(
      /-----BEGIN[A-Z\s]*PRIVATE KEY-----[\s\S]*?(?:-----END[A-Z\s]*PRIVATE KEY-----|$)/g,
      "[REDACTED PRIVATE KEY]",
    )
    .replace(
      /\b(?:proxy-)?authorization["']?\s*[:=][^\r\n]*/gi,
      "Authorization=[REDACTED]",
    )
    .replace(
      /\b[\w.-]*(?:token|password|passwd|secret|credential|api[_-]?key|private[_-]?key)[\w.-]*["']?\s*[:=]\s*(?:"(?:[^"\\]|\\.)*(?:"|$)|'(?:[^'\\]|\\[\s\S])*(?:'|$)|[^\s,;]+)/gi,
      "credential=[REDACTED]",
    )
    .replace(
      /\b(?:https?|postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|amqps?|file):\/\/[^\s"'<>]+/gi,
      "[REDACTED URL]",
    )
    .replace(/\bBasic\s+[A-Za-z0-9+/]+=*/gi, "Basic [REDACTED]")
    .replace(
      /(?:[A-Za-z]:\\|\/)(?:[^\s"'<>]+[\\/])*[^\s"'<>]*/g,
      "[REDACTED PATH]",
    );
  safe = redactSecrets(safe);
  return { text: String(redactText(safe)), incompleteValue };
}

// A physical timestamp is insufficient to establish a new record while inside
// a quoted value or PEM block. Keep that state even after dropping an oversized
// record, so a credential continuation cannot become a false standalone record.
class LogCollector {
  private quote: '"' | "'" | null = null;
  private escaped = false;
  private pem = false;
  private previous = "";
  private chunkSuffix = "";
  private record = "";
  private recordBytes = 0;
  private oversized = false;
  private selected: string[] = [];
  private selectedBytes = 0;
  omittedRecords = 0;
  omissionReason: LogTail["omissionReason"];
  truncated = false;

  private append(fragment: string): void {
    if (this.oversized) return;
    this.recordBytes += Buffer.byteLength(fragment);
    if (this.recordBytes > MAX_LOG_RECORD_BYTES) {
      this.oversized = true;
      this.record = "";
      this.truncated = true;
      this.omissionReason = "record_limit";
      return;
    }
    this.record += fragment;
  }

  private collect(): void {
    if (this.oversized) {
      this.omittedRecords++;
    } else {
      const sanitized = redactLog(this.record);
      if (sanitized.incompleteValue) {
        this.omissionReason = "incomplete_value";
        this.truncated = true;
      }
      for (const line of sanitized.text.split("\n").filter(Boolean)) {
        const size = Buffer.byteLength(line + "\n");
        if (!LOG_RECORD.test(line) || size > MAX_LOG_BYTES) {
          this.omittedRecords++;
          this.truncated = true;
          continue;
        }
        this.selected.push(line + "\n");
        this.selectedBytes += size;
        while (
          this.selected.length > MAX_LOG_LINES ||
          this.selectedBytes > MAX_LOG_BYTES
        ) {
          this.selectedBytes -= Buffer.byteLength(this.selected.shift()!);
          this.omittedRecords++;
          this.truncated = true;
        }
      }
    }
    this.record = "";
    this.recordBytes = 0;
    this.oversized = false;
  }

  consume(text: string): void {
    const joined = this.chunkSuffix + text;
    const prefixLength = this.chunkSuffix.length;
    const begins = new Set(
      Array.from(
        joined.matchAll(/-----BEGIN/g),
        (match) => match.index + "-----BEGIN".length - 1 - prefixLength,
      ),
    );
    this.chunkSuffix = joined.slice(-128);
    let start = 0;
    let previousIndex = -1;
    // Ordinary text cannot change lexical state. Visit delimiters and the
    // final N of BEGIN while retaining physical adjacency across chunks.
    for (const match of text.matchAll(/["'\\\nN-]/g)) {
      const index = match.index;
      const character = match[0];
      if (this.quote) {
        if (this.escaped && index === previousIndex + 1) this.escaped = false;
        else if (character === "\\") this.escaped = true;
        else {
          this.escaped = false;
          if (character === this.quote) this.quote = null;
        }
      } else if (this.pem) {
        if (
          character === "-" &&
          /-----END[A-Z\s]*PRIVATE KEY-----$/.test(
            joined.slice(
              Math.max(0, prefixLength + index + 1 - 128),
              prefixLength + index + 1,
            ),
          )
        ) {
          this.pem = false;
        }
      } else if (
        character === '"' ||
        (character === "'" &&
          /[\s=:]/.test(index === 0 ? this.previous : text[index - 1]!))
      ) {
        this.quote = character;
      } else if (begins.has(index)) {
        this.pem = true;
      }
      previousIndex = index;
      if (character === "\n" && !this.quote && !this.pem) {
        this.append(text.slice(start, index + 1));
        this.collect();
        start = index + 1;
      }
    }
    this.escaped = this.escaped && previousIndex === text.length - 1;
    if (text.length) this.previous = text[text.length - 1]!;
    this.append(text.slice(start));
  }

  finish(): string {
    if (this.recordBytes || this.oversized) {
      this.truncated = true;
      if (this.quote || this.pem) {
        this.omissionReason = "incomplete_value";
        // Retain only complete physical lines before redacting the ambiguous
        // suffix. The redactor withholds it through the snapshot's end.
        this.record = this.record.slice(0, this.record.lastIndexOf("\n") + 1);
        this.collect();
      } else {
        this.omittedRecords++;
      }
    }
    return this.selected.join("");
  }
}

async function readLogTail(profile: string | null): Promise<LogTail> {
  const empty: LogTail = {
    status: profile ? "unavailable" : "unresolved",
    truncated: false,
    omittedRecords: 0,
    text: "",
  };
  if (!profile) return empty;
  const directory = profileDir(profile);
  const path = profileLogPath(profile);
  try {
    // Reject symlinks in the profile path as well as at the final log file.
    if ((await realpath(directory)) !== resolve(directory)) return empty;
    const before = await lstat(path);
    if (!before.isFile()) return empty;
    const handle = await open(
      path,
      constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK,
    );
    try {
      const opened = await handle.stat();
      if (
        !opened.isFile() ||
        opened.ino !== before.ino ||
        opened.dev !== before.dev ||
        (await realpath(directory)) !== resolve(directory)
      )
        return empty;
      const collector = new LogCollector();
      const decoder = new TextDecoder("utf-8", { fatal: true });
      const buffer = Buffer.alloc(LOG_READ_CHUNK_BYTES);
      const sourceBytes = Math.min(opened.size, MAX_LOG_SOURCE_BYTES);
      const deadline = performance.now() + MAX_LOG_READ_MS;
      let position = 0;
      while (position < sourceBytes && performance.now() < deadline) {
        const { bytesRead } = await handle.read(
          buffer,
          0,
          Math.min(buffer.length, sourceBytes - position),
          position,
        );
        if (!bytesRead) break;
        collector.consume(
          decoder.decode(buffer.subarray(0, bytesRead), { stream: true }),
        );
        position += bytesRead;
      }
      try {
        collector.consume(decoder.decode());
      } catch {
        // An append may stop halfway through a UTF-8 character. Keep earlier
        // complete records and withhold the undecodable trailing fragment.
        collector.truncated = true;
        collector.omissionReason = "incomplete_value";
      }
      const after = await lstat(path);
      if (
        !after.isFile() ||
        after.ino !== opened.ino ||
        after.dev !== opened.dev ||
        after.size < opened.size ||
        (await realpath(directory)) !== resolve(directory)
      )
        return empty;
      const text = collector.finish();
      const limited = position < opened.size;
      return {
        status: "available",
        truncated: collector.truncated || limited || after.size > opened.size,
        omittedRecords: collector.omittedRecords,
        omissionReason: limited
          ? position >= MAX_LOG_SOURCE_BYTES
            ? "source_limit"
            : "read_limit"
          : collector.omissionReason,
        text,
      };
    } finally {
      await handle.close();
    }
  } catch {
    // Raw filesystem errors can contain paths and credentials.
    return empty;
  }
}

function safeVersion(value: string | null): string | null {
  return value && /^[0-9][A-Za-z0-9.+_-]{0,95}$/.test(value)
    ? String(redactText(value))
    : null;
}

async function destinationState(path: string) {
  try {
    const info = await lstat(path);
    if (!info.isFile()) throw new Error("Destination must be a regular file");
    return `${info.dev}:${info.ino}:${info.size}:${info.mtimeMs}`;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return null;
    throw error;
  }
}

export function setupDiagnosticsExport(
  snapshot: () => DiagnosticsSnapshot,
): void {
  let exporting = false;
  ipcMain.handle(
    DIAGNOSTICS_EXPORT_CHANNEL,
    async (event): Promise<DiagnosticsExportResult> => {
      if (exporting) return { status: "busy" };
      exporting = true;
      let temporaryDirectory: string | undefined;
      try {
        const options: SaveDialogOptions = {
          defaultPath: "multica-diagnostics.zip",
          filters: [{ name: "ZIP", extensions: ["zip"] }],
          properties: ["showOverwriteConfirmation"],
        };
        const window = BrowserWindow.fromWebContents(event.sender);
        const destination = window
          ? await dialog.showSaveDialog(window, options)
          : await dialog.showSaveDialog(options);
        if (destination.canceled || !destination.filePath)
          return { status: "cancelled" };
        const originalDestination = await destinationState(
          destination.filePath,
        );
        const captured = snapshot();
        const log = await readLogTail(captured.profile);
        if (snapshot().profile !== captured.profile)
          return { status: "error", reason: "profile_changed" };
        const appVersion = safeVersion(app.getVersion());
        const cliVersion = safeVersion(captured.cliVersion);
        const metadata = Buffer.from(
          JSON.stringify(
            {
              schemaVersion: 1,
              createdAt: new Date().toISOString(),
              appVersion,
              cliVersion,
              profile: { resolved: captured.profile !== null },
              daemon: { lastObservedState: captured.state },
              log: {
                status: log.status,
                truncated: log.truncated,
                omittedRecords: log.omittedRecords,
                omissionReason: log.omissionReason ?? null,
                sourceBytesLimit: MAX_LOG_SOURCE_BYTES,
                bytes: Buffer.byteLength(log.text),
              },
            },
            null,
            2,
          ) + "\n",
        );
        if (metadata.length > MAX_METADATA_BYTES)
          return { status: "error", reason: "save_failed" };
        const archive = writeZip([
          { path: "diagnostics.json", data: metadata },
          { path: "daemon.log", data: Buffer.from(log.text) },
        ]);
        if (archive.byteLength > MAX_ARCHIVE_BYTES)
          return { status: "error", reason: "save_failed" };
        temporaryDirectory = await mkdtemp(
          join(dirname(destination.filePath), ".multica-diagnostics-"),
        );
        const temporaryFile = join(temporaryDirectory, "archive.zip");
        const file = await open(temporaryFile, "wx", 0o600);
        try {
          await file.writeFile(archive);
          await file.sync();
        } finally {
          await file.close();
        }
        if (snapshot().profile !== captured.profile)
          return { status: "error", reason: "profile_changed" };
        if (
          (await destinationState(destination.filePath)) !== originalDestination
        )
          return { status: "error", reason: "save_failed" };
        await rename(temporaryFile, destination.filePath);
        return {
          status: "saved",
          partial:
            log.status !== "available" ||
            log.truncated ||
            cliVersion === null ||
            appVersion === null,
        };
      } catch {
        return { status: "error", reason: "save_failed" };
      } finally {
        if (temporaryDirectory)
          await rm(temporaryDirectory, { recursive: true, force: true }).catch(
            () => {},
          );
        exporting = false;
      }
    },
  );
}
