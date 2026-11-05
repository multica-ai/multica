import { requireOptionalNativeModule } from "expo";
import type { AISessionClient } from "./client";

interface NativeFiles { retainSubmissionAsset(key:string,uri:string):Promise<string>; removeSubmissionAsset(key:string):Promise<void>; readSubmissions(key:string):Promise<string|null>; writeSubmissions(key:string,content:string):Promise<void>; sha256File(uri: string): Promise<string | null>; readSnapshot(key: string): Promise<string | null>; writeSnapshot(key: string, content: string): Promise<void>; clearSnapshots(): Promise<void>; clearFiles(): Promise<void>; openFile(url: string, headers: Record<string, string>, name: string, share: boolean): Promise<void> }
const native = requireOptionalNativeModule<NativeFiles>("MulticaDeviceLink");

export function clearDeviceFiles() {
  void native?.clearFiles().catch(() => {});
  void native?.clearSnapshots().catch(() => {});
}

/** The file's SHA-256; null when it cannot be read here, which only costs a transfer. */
export async function fileContentHash(uri: string): Promise<string | null> {
  return native ? native.sha256File(uri).catch(() => null) : null;
}

export async function readStoredSnapshot(key: string): Promise<string | null> {
  return native ? native.readSnapshot(key).catch(() => null) : null;
}

export function writeStoredSnapshot(key: string, content: string) { void native?.writeSnapshot(key, content).catch(() => {}); }

export async function openDeviceFile(client: AISessionClient, path: string, name = "", share = false) {
  if (!native) throw new Error("native_unavailable");
  await native.openFile(client.url(path), client.headers(), name, share);
}

export function remoteWebURL(value: string): string | null {
  try {
    const url = new URL(value);
    const host = url.hostname.toLowerCase();
    if (!["https:", "http:"].includes(url.protocol) || url.username || url.password) return null;
    if (host === "localhost" || host.endsWith(".localhost") || /^127\./.test(host) || ["::1", "[::1]", "0.0.0.0"].includes(host)) return null;
    return url.href;
  } catch { return null; }
}

/** Reliable writes reject; the composer must retain its draft if the disk fails. */
export async function readStoredSubmissions(key:string) {
  if(!native)throw new Error("native_unavailable");
  return native.readSubmissions(key);
}
export async function writeStoredSubmissions(key:string,content:string) {
  if(!native)throw new Error("native_unavailable");
  await native.writeSubmissions(key,content);
}

export async function retainSubmissionAsset(key:string,uri:string){if(!native)throw new Error("native_unavailable");return native.retainSubmissionAsset(key,uri);}
export async function removeSubmissionAsset(key:string){if(!native)throw new Error("native_unavailable");await native.removeSubmissionAsset(key);}
