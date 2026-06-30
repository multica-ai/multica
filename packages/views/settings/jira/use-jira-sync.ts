import { useCallback, useState } from "react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import {
  syncJiraIssues,
  type JiraConfig,
  type JiraTransport,
  type SyncResult,
} from "@multica/core/jira";

/** Drives a user-triggered Jira → Multica sync from the desktop renderer. Reads
 *  the main-process Jira config + transport via `window.jiraAPI`, resolves the
 *  current member from the auth store, and runs the core sync engine against
 *  the shared ApiClient. Returns null (with `error` set) when not on desktop. */
export function useJiraSync() {
  const user = useAuthStore((s) => s.user);
  const [running, setRunning] = useState(false);
  const [lastResult, setLastResult] = useState<SyncResult | null>(null);
  const [error, setError] = useState<string | null>(null);

  const syncNow = useCallback(async (): Promise<SyncResult | null> => {
    if (typeof window === "undefined" || !window.jiraAPI) {
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
      const cfg = await window.jiraAPI.getConfig();
      const transport: JiraTransport = (req) => window.jiraAPI.request(req);
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

  return { syncNow, running, lastResult, error };
}
