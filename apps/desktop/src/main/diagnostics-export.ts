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
  text: string;
}

function redactLog(text: string): string {
  // Quoted values may span lines. Redact before selecting individual records.
  let safe = text
    .replace(
      /-----BEGIN[A-Z\s]*PRIVATE KEY-----[\s\S]*?(?:-----END[A-Z\s]*PRIVATE KEY-----|$)/g,
      "[REDACTED PRIVATE KEY]",
    )
    .replace(
      /\b(?:proxy-)?authorization["']?\s*[:=][^\r\n]*/gi,
      "Authorization=[REDACTED]",
    )
    .replace(
      /\b[\w.-]*(?:token|password|passwd|secret|credential|api[_-]?key|private[_-]?key)[\w.-]*["']?\s*[:=]\s*(?:"(?:[^"\\]|\\.)*(?:"|$)|'(?:[^'\\]|\\.)*(?:'|$)|[^\s,;]+)/gi,
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
  return String(redactText(safe));
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
      const start = Math.max(0, opened.size - MAX_LOG_BYTES);
      const buffer = Buffer.alloc(Math.min(opened.size, MAX_LOG_BYTES));
      const { bytesRead } = await handle.read(buffer, 0, buffer.length, start);
      const after = await lstat(path);
      if (
        !after.isFile() ||
        after.ino !== opened.ino ||
        after.dev !== opened.dev ||
        after.size < opened.size
      )
        return empty;
      // Drop both boundary fragments, including a concurrent incomplete append.
      const partialLastLine = bytesRead > 0 && buffer[bytesRead - 1] !== 0x0a;
      let text = buffer.subarray(0, bytesRead).toString("utf8");
      if (start > 0) text = text.slice(text.indexOf("\n") + 1);
      text = text.slice(0, text.lastIndexOf("\n") + 1);
      const redacted = redactLog(text).split("\n").filter(Boolean);
      const records = redacted.filter((line) => LOG_RECORD.test(line));
      const selected: string[] = [];
      let bytes = 0;
      for (const line of records.slice(-MAX_LOG_LINES).reverse()) {
        const size = Buffer.byteLength(line + "\n");
        if (bytes + size > MAX_LOG_BYTES) break;
        selected.push(line);
        bytes += size;
      }
      return {
        status: "available",
        truncated:
          start > 0 ||
          partialLastLine ||
          after.size > opened.size ||
          bytesRead < buffer.length ||
          selected.length < records.length,
        omittedRecords: redacted.length - selected.length,
        text: selected
          .reverse()
          .map((line) => line + "\n")
          .join(""),
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
        const metadata = Buffer.from(
          JSON.stringify(
            {
              schemaVersion: 1,
              createdAt: new Date().toISOString(),
              appVersion: safeVersion(app.getVersion()),
              cliVersion: safeVersion(captured.cliVersion),
              profile: { resolved: captured.profile !== null },
              daemon: { lastObservedState: captured.state },
              log: {
                status: log.status,
                truncated: log.truncated,
                omittedRecords: log.omittedRecords,
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
        return { status: "saved" };
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
