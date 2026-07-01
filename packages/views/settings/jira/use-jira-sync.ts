import { useCallback, useState } from "react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import {
  syncJiraIssues,
  clearSyncedJiraIssues,
  type JiraConfig,
  type JiraTransport,
  type SyncResult,
} from "@multica/core/jira";

/** Non-secret Jira config the desktop main process returns to the renderer.
 *  `apiToken` is always "" when read back; `hasToken` reflects whether one is
 *  stored. Declared here (not imported from the desktop package) because views
 *  must stay platform-agnostic. */
export interface JiraDesktopConfig {
  siteUrl: string;
  email: string;
  apiToken: string;
  hasToken: boolean;
  jql: string;
  statusMapping: Record<string, string>;
  pollIntervalMinutes: number;
}

/** Bridge exposed by the Electron preload on desktop; undefined on web. */
export interface JiraDesktopBridge {
  request: (req: { method: string; path: string; body?: unknown }) => Promise<unknown>;
  getConfig: () => Promise<JiraDesktopConfig>;
  setConfig: (patch: Partial<JiraDesktopConfig>) => Promise<JiraDesktopConfig>;
  onPollTick: (callback: () => void) => () => void;
}

/** Read the desktop Jira bridge without augmenting the global `Window` type —
 *  views stays platform-agnostic and avoids colliding with the desktop app's
 *  own `Window.jiraAPI` declaration when both are compiled together. Returns
 *  undefined on web (and in tests where it isn't installed). */
export function getJiraBridge(): JiraDesktopBridge | undefined {
  if (typeof window === "undefined") return undefined;
  return (window as unknown as { jiraAPI?: JiraDesktopBridge }).jiraAPI;
}

/** Drives a user-triggered Jira → Multica sync from the desktop renderer. Reads
 *  the main-process Jira config + transport via `window.jiraAPI`, resolves the
 *  current member from the auth store, and runs the core sync engine against
 *  the shared ApiClient. Returns null (with `error` set) when not on desktop. */
export function useJiraSync() {
  const user = useAuthStore((s) => s.user);
  const [running, setRunning] = useState(false);
  const [clearing, setClearing] = useState(false);
  const [lastResult, setLastResult] = useState<SyncResult | null>(null);
  const [clearedCount, setClearedCount] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  const syncNow = useCallback(async (): Promise<SyncResult | null> => {
    const bridge = getJiraBridge();
    if (!bridge) {
      setError("Jira sync is only available in the desktop app.");
      return null;
    }
    if (!user) {
      setError("You must be signed in to sync Jira issues.");
      return null;
    }
    setRunning(true);
    setError(null);
    try {
      const cfg = await bridge.getConfig();
      const transport: JiraTransport = (req) => bridge.request(req);
      const config: JiraConfig = {
        siteUrl: cfg.siteUrl,
        email: cfg.email,
        jql: cfg.jql,
        statusMapping: cfg.statusMapping as JiraConfig["statusMapping"],
        pollIntervalMinutes: cfg.pollIntervalMinutes,
      };
      const result = await syncJiraIssues({
        transport,
        api,
        config,
        currentMemberId: user.id,
      });
      setLastResult(result);
      return result;
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      return null;
    } finally {
      setRunning(false);
    }
  }, [user]);

  /** Delete all previously synced Jira issues so the next sync starts clean.
   *  Returns the number deleted, or null on error / when not on desktop. */
  const clearSynced = useCallback(async (): Promise<{ deleted: number } | null> => {
    const bridge = getJiraBridge();
    if (!bridge) {
      setError("Jira sync is only available in the desktop app.");
      return null;
    }
    setClearing(true);
    setError(null);
    setClearedCount(null);
    try {
      const result = await clearSyncedJiraIssues(api);
      setClearedCount(result.deleted);
      return result;
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      return null;
    } finally {
      setClearing(false);
    }
  }, []);

  return { syncNow, clearSynced, running, clearing, lastResult, clearedCount, error };
}
