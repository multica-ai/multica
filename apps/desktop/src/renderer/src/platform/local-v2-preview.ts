import { z } from "zod";
import type { LocalPreviewFile, LocalPreviewLocation } from "@multica/core/local-conversation/protocol";
import type { LocalV2Scope, LocalV2Transport } from "@multica/core/local-conversation/v2";
import { conversationStore, type ConversationStore } from "./conversation-store";

const TEXT_LIMIT = 1024 * 1024;
const locationSchema = z.object({ root: z.string(), path: z.string(), name: z.string(), kind: z.enum(["file", "directory"]) });
const fileSchema = z.object({ root: z.string(), path: z.string(), name: z.string(), mime_type: z.string(), size: z.number() });

const query = (values: Record<string, string | undefined>) => new URLSearchParams(Object.entries(values).filter((entry): entry is [string, string] => !!entry[1])).toString();

/**
 * Read-only file preview over the daemon: the conversation's working
 * directory and folders the user chose are readable. Errors keep the
 * daemon's kind, e.g. not_found or outside_folder.
 */
export class LocalV2Preview {
  private client: ConversationStore;
  constructor(private transport: LocalV2Transport, private scope: LocalV2Scope) { this.client = conversationStore(transport, scope); }

  async locate(conversationId: string, path: string): Promise<LocalPreviewLocation> {
    return locationSchema.parse(await this.client.request("GET", `/preview/locate?${query({ conversation_id: conversationId, path })}`));
  }

  async list(location: { root: string; path: string }, conversationId?: string): Promise<LocalPreviewLocation[]> {
    const raw = await this.client.request("GET", `/preview/list?${query({ conversation_id: conversationId, root: location.root, path: location.path })}`);
    return z.object({ entries: z.array(locationSchema) }).parse(raw).entries;
  }

  /** A file's metadata, with images and PDFs by URL and text up to 1 MB. */
  async read(location: { root: string; path: string }, conversationId?: string): Promise<LocalPreviewFile> {
    const params = { conversation_id: conversationId, root: location.root, path: location.path };
    const file = fileSchema.parse(await this.client.request("GET", `/preview/file?${query(params)}`));
    const preview: LocalPreviewFile = { name: file.name, path: file.path, root: file.root, mimeType: file.mime_type, size: file.size, truncated: false };
    const url = this.client.fileUrl(`/preview/content?${query(params)}`);
    if (file.mime_type.startsWith("image/") || file.mime_type === "application/pdf") return { ...preview, url };
    const response = await fetch(url, { headers: { Range: `bytes=0-${TEXT_LIMIT}` } });
    if (!response.ok) throw new Error(`Local file request failed (HTTP ${response.status})`);
    const bytes = new Uint8Array(await response.arrayBuffer());
    return { ...preview, text: new TextDecoder().decode(bytes.subarray(0, TEXT_LIMIT)), truncated: file.size > TEXT_LIMIT };
  }

  /** Lets the user choose a file or folder to preview; null when cancelled. */
  async choose(kind: "file" | "directory"): Promise<LocalPreviewLocation | null> {
    const raw = await this.transport.choose(this.scope, { purpose: "preview", kind });
    return raw ? locationSchema.parse(raw) : null;
  }
}

export const localV2Preview = (transport: LocalV2Transport, scope: LocalV2Scope) => new LocalV2Preview(transport, scope);
