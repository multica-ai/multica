"use client";

import { useEffect, useState } from "react";
import type { WorktreeReadiness } from "@multica/core/types";
import { useT } from "../../i18n";

export interface WorktreeReadinessNoticeProps {
  readiness?: WorktreeReadiness;
  /** False for an unsaved configuration, which has no matching measurement. */
  saved?: boolean;
  /** Failed list refreshes must not leave a cached ready badge on screen. */
  unavailable?: boolean;
}

export function WorktreeReadinessNotice({
  readiness,
  saved = true,
  unavailable = false,
}: WorktreeReadinessNoticeProps) {
  const { t, i18n } = useT("projects");
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    // Expire cached readiness even if a poll fails or returns identical data.
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);

  const expiresAt = readiness?.expires_at ? Date.parse(readiness.expires_at) : NaN;
  const expired = !Number.isFinite(expiresAt) || expiresAt <= now;
  const status = !saved ? "unchecked"
    : unavailable || !readiness ? "unavailable"
    : (readiness.status === "ready" || readiness.status === "blocked") && expired
      ? "unavailable" : readiness.status;
  const titles = {
    unchecked: t(($) => $.resources.readiness.unchecked),
    checking: t(($) => $.resources.readiness.checking),
    ready: t(($) => $.resources.readiness.ready),
    blocked: t(($) => $.resources.readiness.blocked),
    unavailable: t(($) => $.resources.readiness.unavailable),
  };
  const number = (value: number) => value.toLocaleString(i18n.language);
  const bytes = (value: number) => t(($) => $.resources.readiness.bytes, {
    bytes: number(value),
    mib: (value / 1048576).toLocaleString(i18n.language, { maximumFractionDigits: 1 }),
  });
  const unknown = t(($) => $.resources.readiness.not_measured);
  const measurements = saved && readiness && status !== "checking" ? readiness : undefined;
  const hasUsage = measurements?.file_count !== undefined || measurements?.total_bytes !== undefined;

  return (
    <div className="min-w-0 space-y-1 rounded-md border p-2 text-micro">
      <p role="status" className={status === "blocked" ? "font-medium text-warning" : "font-medium"}>
        {titles[status] ?? titles.unavailable}
      </p>
      {status === "unchecked" && <p>{t(($) => $.resources.readiness.save_to_check)}</p>}
      {status === "checking" && <p>{t(($) => $.resources.readiness.awaiting)}</p>}
      {status === "unavailable" && (
        <p>{readiness?.reason_code === "stale_measurement" || (readiness?.checked_at && expired)
          ? t(($) => $.resources.readiness.stale)
          : readiness?.reason_code === "inspection_failed"
            ? t(($) => $.resources.readiness.inspection_failed)
            : t(($) => $.resources.readiness.connect)}</p>
      )}
      {status === "blocked" && <p>{t(($) => $.resources.readiness.cleanup)}</p>}
      {measurements && hasUsage && (
        <details open={status !== "ready"}>
          <summary className="cursor-pointer text-muted-foreground">
            {status === "unavailable"
              ? t(($) => $.resources.readiness.last_measured)
              : t(($) => $.resources.readiness.measurements)}
          </summary>
          <div className="mt-1 space-y-1 break-words">
            <p>{t(($) => $.resources.readiness.files, {
              value: measurements.file_count === undefined ? unknown : number(measurements.file_count),
              limit: measurements.max_files === undefined ? unknown : number(measurements.max_files),
            })}</p>
            <p>{t(($) => $.resources.readiness.size, {
              value: measurements.total_bytes === undefined ? unknown : bytes(measurements.total_bytes),
              limit: measurements.max_bytes === undefined ? unknown : bytes(measurements.max_bytes),
            })}</p>
            {!!measurements.symlink_count && <p>{t(($) => $.resources.readiness.symlinks, { value: number(measurements.symlink_count) })}</p>}
            {!!measurements.largest_paths?.length && (
              <div>
                <p className="text-muted-foreground">{t(($) => $.resources.readiness.largest)}</p>
                <ul className="space-y-1">
                  {measurements.largest_paths.map((path) => (
                    <li key={path.path}>
                      <span className="font-mono break-all">{path.path}</span>
                      <span className="block text-muted-foreground">{t(($) => $.resources.readiness.path_usage, { files: number(path.file_count), bytes: bytes(path.total_bytes) })}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {measurements.checked_at && <p className="text-muted-foreground">{t(($) => $.resources.readiness.checked_at, { time: new Date(measurements.checked_at).toLocaleString(i18n.language) })}</p>}
          </div>
        </details>
      )}
    </div>
  );
}
