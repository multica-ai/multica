import { useCallback, useEffect, useRef } from "react";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { getOrCreateInstallId, utcDay } from "@multica/core/client-usage";
import { defaultStorage } from "@multica/core/platform";
import type { LocalRuntimeProbe } from "../../../shared/daemon-types";

const LAST_RUNTIME_PREFIX = "multica_runtime_probe_last_reported";

export function runtimeProbeSignature(probe: LocalRuntimeProbe): string {
  if (probe.probeResult === "error") return "error";
  return JSON.stringify({
    runtimeCount: probe.runtimeCount,
    providers: Object.entries(probe.providerSummary).sort(([a], [b]) =>
      a.localeCompare(b),
    ),
    onlineCount: probe.onlineCount,
    offlineCount: probe.offlineCount,
  });
}

export function DesktopClientUsageReporter({ apiUrl }: { apiUrl: string }) {
  const userID = useAuthStore((state) => state.user?.id ?? null);
  const userIDRef = useRef(userID);
  userIDRef.current = userID;
  const inFlight = useRef(false);
  const rerun = useRef(false);
  const lastStatusSignal = useRef<string | null>(null);

  const probeAndReport = useCallback(async () => {
    const activeUserID = userIDRef.current;
    if (!activeUserID) return;
    if (inFlight.current) {
      rerun.current = true;
      return;
    }
    inFlight.current = true;
    try {
      await window.daemonAPI.setTargetApiUrl(apiUrl);
      const installID = getOrCreateInstallId(defaultStorage);
      const probe = await window.daemonAPI.probeRuntimes();
      if (userIDRef.current !== activeUserID) {
        rerun.current = true;
        return;
      }
      const day = utcDay();
      const signature = runtimeProbeSignature(probe);
      const key = `${LAST_RUNTIME_PREFIX}:${activeUserID}:${installID}`;
      if (defaultStorage.getItem(key) === `${day}:${signature}`) return;

      await api.upsertClientUsage({
        install_id: installID,
        runtime:
          probe.probeResult === "error"
            ? { probe_result: "error" }
            : {
                probe_result: "success",
                runtime_count: probe.runtimeCount,
                provider_summary: probe.providerSummary,
                online_count: probe.onlineCount,
                offline_count: probe.offlineCount,
              },
      });
      defaultStorage.setItem(key, `${day}:${signature}`);
    } catch {
      // Runtime analytics are best-effort and never block daemon control.
    } finally {
      inFlight.current = false;
      if (rerun.current) {
        rerun.current = false;
        void probeAndReport();
      }
    }
  }, [apiUrl]);

  useEffect(() => {
    void probeAndReport();
  }, [probeAndReport, userID]);

  useEffect(() => {
    const unsubscribe = window.daemonAPI.onStatusChange((status) => {
      if (
        status.state === "running" ||
        status.state === "stopped" ||
        status.state === "auth_expired" ||
        status.state === "cli_not_found"
      ) {
        const signal = `${status.state}:${[...(status.agents ?? [])].sort().join(",")}`;
        if (lastStatusSignal.current === signal) return;
        lastStatusSignal.current = signal;
        void probeAndReport();
      }
    });
    const onFocus = () => void probeAndReport();
    window.addEventListener("focus", onFocus);
    return () => {
      unsubscribe();
      window.removeEventListener("focus", onFocus);
    };
  }, [probeAndReport]);

  return null;
}
