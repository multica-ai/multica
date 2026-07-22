/**
 * Mirrors packages/views/common/use-viewing-timezone.ts's fallback chain:
 * stored user preference, else device-detected, else UTC. Behavioral
 * parity requires the Usage page slice the same "today" boundary as web
 * for the same account, so this must not fall back to a different
 * default.
 */
import * as Localization from "expo-localization";
import { useAuthStore } from "@/data/auth-store";

export function useViewingTimezone(): string {
  const stored = useAuthStore((s) => s.user?.timezone ?? null);
  if (stored && stored.trim() !== "") return stored;
  return Localization.getCalendars()[0]?.timeZone ?? "UTC";
}
