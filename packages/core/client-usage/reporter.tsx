"use client";

import { useCallback, useEffect, useRef } from "react";
import { getApi } from "../api";
import { useAuthStore } from "../auth";
import type { StorageAdapter } from "../types/storage";
import type { ClientIdentity } from "../platform/types";
import { getOrCreateInstallId } from "./install-id";

const LAST_REPORTED_PREFIX = "multica_client_usage_last_reported";

export function utcDay(date = new Date()): string {
  return date.toISOString().slice(0, 10);
}

export function ClientUsageReporter({
  storage,
  identity,
}: {
  storage: StorageAdapter;
  identity?: ClientIdentity;
}) {
  const userID = useAuthStore((state) => state.user?.id ?? null);
  const userIDRef = useRef(userID);
  userIDRef.current = userID;
  const inFlight = useRef(false);
  const rerun = useRef(false);

  const reportIfNeeded = useCallback(async () => {
    const platform = identity?.platform;
    const activeUserID = userIDRef.current;
    if (!activeUserID || (platform !== "web" && platform !== "desktop"))
      return;
    if (inFlight.current) {
      rerun.current = true;
      return;
    }

    const day = utcDay();
    try {
      const installID = getOrCreateInstallId(storage);
      const reportedKey = `${LAST_REPORTED_PREFIX}:${activeUserID}:${platform}:${installID}`;
      if (storage.getItem(reportedKey) === day) return;
      inFlight.current = true;
      await getApi().upsertClientUsage({ install_id: installID });
      storage.setItem(reportedKey, day);
    } catch {
      // Usage reporting is best-effort and must never interrupt app startup.
    } finally {
      inFlight.current = false;
      if (rerun.current) {
        rerun.current = false;
        void reportIfNeeded();
      }
    }
  }, [identity?.platform, storage]);

  useEffect(() => {
    void reportIfNeeded();
  }, [reportIfNeeded, userID]);

  useEffect(() => {
    const onFocus = () => void reportIfNeeded();
    const onVisibility = () => {
      if (document.visibilityState === "visible") void reportIfNeeded();
    };
    window.addEventListener("focus", onFocus);
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      window.removeEventListener("focus", onFocus);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [reportIfNeeded]);

  return null;
}
