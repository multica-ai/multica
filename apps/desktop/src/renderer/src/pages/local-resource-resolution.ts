import type { LocalPreviewFile, LocalPreviewLocation } from "@multica/core/local-conversation/protocol";
import { localResourceTarget } from "@multica/tutti-agent-gui-host/message-resource-links";
import type { ConversationModel } from "@multica/conversation-client/models";
import { LocalV2RequestError } from "@multica/conversation-client/services";

export class FileLinkPreviewError extends Error {
  constructor(readonly kind: "missing" | "ambiguous" | "denied" | "incomplete") {
    super(`File preview: ${kind}`);
  }
}

const absolute = (path: string) => /^(?:\/|[a-z]:[\\/])/i.test(path);
const join = (root: string, path: string) => `${root.replace(/[\\/]+$/, "")}/${path}`;
const missing = (error: unknown) => error instanceof LocalV2RequestError && error.kind === "not_found";
const denied = (error: unknown) => error instanceof LocalV2RequestError && error.kind === "outside_folder";

/** What a conversation cites: its message texts and its tools' inputs. */
export interface ResolutionSource { cwd: string; texts: string[]; toolInputs: Record<string, unknown>[] }

export function resolutionSource(conversation: ConversationModel): ResolutionSource {
  const items = [...conversation.items.values()].sort((a, b) => a.order - b.order);
  return {
    cwd: conversation.summary.cwd,
    texts: [...[...conversation.messages.values()].map((message) => message.content), ...items.flatMap((item) => item.kind === "assistant_text" ? [item.text] : [])],
    toolInputs: items.flatMap((item) => item.kind === "command" ? [{ command: item.state.command }]
      : item.kind === "tool_call" && item.state.input && typeof item.state.input === "object" ? [item.state.input as Record<string, unknown>] : []),
  };
}

interface Input {
  source: ResolutionSource;
  value: string;
  read(path: string): Promise<LocalPreviewFile>;
  list(path: string): Promise<LocalPreviewLocation[]>;
}

/** Resolve authored references through the existing reader, which retains the access boundary. */
export async function resolveConversationFile({ source, value, read, list }: Input): Promise<{ file: LocalPreviewFile; line?: number }> {
  const target = localResourceTarget(value);
  if (!target) throw new Error("Use a local file path or webpage address");
  const result = (file: LocalPreviewFile) => ({ file, ...(target.line ? { line: target.line } : {}) });
  try { return result(await read(absolute(target.path) ? target.path : join(source.cwd, target.path))); }
  catch (error) {
    if (denied(error)) throw new FileLinkPreviewError("denied");
    if (!missing(error)) throw error;
  }
  // Explicit absolute locations and parent traversal must never select another file.
  if (absolute(target.path) || target.path.split("/").includes("..")) throw new FileLinkPreviewError("missing");

  const suffix = target.path.replace(/^(?:\.\/)+/, "");
  const hints = new Set<string>();
  const addHint = (value: string, absoluteOnly = false) => {
    const hint = localResourceTarget(value)?.path;
    if (!hint || (absoluteOnly && !absolute(hint)) || !hint.endsWith(`/${suffix}`) || hint.split("/").includes("..")) return;
    hints.add(hint);
    if (hints.size > 8) throw new FileLinkPreviewError("incomplete");
  };
  if (source.texts.reduce((size, text) => size + text.length, 0) > 2_000_000) throw new FileLinkPreviewError("incomplete");
  for (const text of source.texts) {
    for (const match of text.matchAll(/`([^`\r\n]+)`|\]\((?:<([^>\r\n]+)>|([^\s)]+))\)/g)) {
      addHint(match[1] ?? match[2] ?? match[3] ?? "");
    }
  }
  // Replies can abbreviate files that tools addressed by absolute path. These
  // inputs are hints only: never execute commands or infer shell cwd changes.
  let inputSize = 0;
  for (const input of source.toolInputs) {
    {
      for (const value of Object.values(input)) {
        if (typeof value !== "string") continue;
        inputSize += value.length;
        if (inputSize > 2_000_000) throw new FileLinkPreviewError("incomplete");
        addHint(value, true);
        for (const match of value.matchAll(/"([^"\r\n]+)"|'([^'\r\n]+)'|([^\s"'`;|&<>]+)/g)) {
          addHint(match[1] ?? match[2] ?? match[3] ?? "", true);
        }
      }
    }
  }
  if (!hints.size) throw new FileLinkPreviewError("missing");
  const found = new Map<string, LocalPreviewFile>();
  let inaccessible = false;
  let unresolved = 0;
  const probe = async (paths: string[]) => {
    for (let index = 0; index < paths.length; index += 4) {
      await Promise.all(paths.slice(index, index + 4).map(async (path) => {
        try { const file = await read(path); found.set(file.path, file); }
        catch (error) {
          unresolved++;
          if (denied(error)) inaccessible = true;
          else if (!missing(error)) throw error;
        }
      }));
      if (found.size > 1) throw new FileLinkPreviewError("ambiguous");
    }
  };
  await probe([...hints].map((hint) => absolute(hint) ? hint : join(source.cwd, hint)));
  if (hints.size > 1 && (found.size !== 1 || unresolved > 0)) throw new FileLinkPreviewError("ambiguous");
  if (found.size) return result([...found.values()][0]!);

  // Workspace roots can contain several projects. Probe only explicitly cited
  // suffixes in their direct children; do not recursively scan a user's workspace.
  const relativeHints = [...hints].filter((hint) => !absolute(hint));
  if (relativeHints.length) {
    let entries: LocalPreviewLocation[];
    try { entries = await list(source.cwd); }
    catch (error) {
      if (denied(error)) throw new FileLinkPreviewError("denied");
      if (missing(error)) throw new FileLinkPreviewError("missing");
      throw error;
    }
    const folders = entries.filter((entry) => entry.kind === "directory" && !entry.name.startsWith(".") && entry.name !== "node_modules");
    if (entries.length >= 1000 || folders.length * relativeHints.length > 512) throw new FileLinkPreviewError("incomplete");
    await probe(folders.flatMap((folder) => relativeHints.map((hint) => join(folder.path, hint))));
  }
  if (found.size) return result([...found.values()][0]!);
  throw new FileLinkPreviewError(inaccessible ? "denied" : "missing");
}

export function fileLinkErrorMessage(error: FileLinkPreviewError, chinese: boolean): string {
  if (error.kind === "ambiguous") return chinese ? "找到多个匹配文件，请使用完整路径，或从右侧菜单打开文件。" : "Several files match. Use a full path or open a file from the workspace menu.";
  if (error.kind === "denied") return chinese ? "无法访问此文件，请从右侧菜单选择文件或所在文件夹。" : "This file is not accessible. Choose the file or its folder from the workspace menu.";
  if (error.kind === "incomplete") return chinese ? "未能确定文件位置，请使用完整路径，或从右侧菜单打开文件。" : "The file location could not be determined. Use a full path or open a file from the workspace menu.";
  return chinese ? "找不到此文件，请确认文件仍然存在，并使用完整路径，或从右侧菜单打开文件。" : "File not found. Check that it still exists, then use a full path or open it from the workspace menu.";
}
