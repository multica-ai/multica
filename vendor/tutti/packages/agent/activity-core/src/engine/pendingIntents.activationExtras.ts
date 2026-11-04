import type { SessionActivationRequestedIntent } from "./pendingIntents.types.ts";

export function pendingActivationRailSectionKeyFields(
  intent: Pick<SessionActivationRequestedIntent, "railSectionKey">
): { railSectionKey?: string } {
  const railSectionKey = intent.railSectionKey?.trim();
  return railSectionKey ? { railSectionKey } : {};
}
