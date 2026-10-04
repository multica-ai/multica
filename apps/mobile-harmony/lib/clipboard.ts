/**
 * expo-clipboard-shaped clipboard over the MulticaClipboard TurboModule
 * (system pasteboard). No-ops with an empty read when the native module is
 * unavailable.
 */
import { clipboard } from "@/lib/native-modules";

export async function setStringAsync(text: string): Promise<boolean> {
  if (!clipboard) return false;
  return clipboard.setString(text);
}

export async function getStringAsync(): Promise<string> {
  if (!clipboard) return "";
  return clipboard.getString();
}
