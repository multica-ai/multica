"use client";

import { useCallback, useEffect, useState } from "react";
import type { AgentRuntime } from "@multica/core/types";
import { useLocalDaemonStatus } from "./use-local-daemon-status";
import { useOpenLocalFileStore } from "@multica/core/platform/open-local-file-store";
import {
  isOpenLocalFileSupported,
  listLocalFileOpeners,
  openLocalFile,
  pickEffectiveOpener,
  resolveLocalFileCandidate,
  resolveLocalFiles,
  type ListLocalFileOpenersResult,
  type LocalFileOpener,
  type OpenLocalFileResult,
} from "./open-local-file";

export function useLocalFileOpeners(): {
  supported: boolean;
  openers: LocalFileOpener[];
  platform: ListLocalFileOpenersResult["platform"] | null;
  effective: LocalFileOpener | undefined;
  setOpenerId: (openerId: string) => void;
} {
  const supported = isOpenLocalFileSupported();
  const storedId = useOpenLocalFileStore((s) => s.openerId);
  const setOpenerId = useOpenLocalFileStore((s) => s.setOpenerId);
  const [listed, setListed] = useState<ListLocalFileOpenersResult | null>(null);

  useEffect(() => {
    if (!supported) return;
    let cancelled = false;
    void listLocalFileOpeners().then((result) => {
      if (!cancelled && result) setListed(result);
    });
    return () => {
      cancelled = true;
    };
  }, [supported]);

  const openers = listed?.openers ?? [];
  const effective = pickEffectiveOpener(openers, storedId);

  return {
    supported,
    openers,
    platform: listed?.platform ?? null,
    effective,
    setOpenerId,
  };
}

export function useExistingLocalFilePaths(
  editPaths: readonly string[],
  workdir?: string,
  runtime?: Pick<AgentRuntime, "id" | "runtime_mode" | "daemon_id"> | null,
): Set<string> {
  const { daemonId } = useLocalDaemonStatus();
  const isLocalRuntime = Boolean(daemonId && runtime?.runtime_mode === "local" && runtime.daemon_id === daemonId);
  const supported = isOpenLocalFileSupported();
  const candidateKey = JSON.stringify([runtime?.id, daemonId, isLocalRuntime, workdir, editPaths]);
  const [resolved, setResolved] = useState<{ key: string; paths: Set<string> } | null>(null);

  useEffect(() => {
    const candidates = [
      ...new Set(
        editPaths
          .map((path) => resolveLocalFileCandidate(path, workdir))
          .filter((path): path is string => path !== null),
      ),
    ];
    if (!supported || !isLocalRuntime || candidates.length === 0) {
      setResolved(null);
      return;
    }
    let cancelled = false;
    void resolveLocalFiles(candidates).then((found) => {
      if (!cancelled) setResolved({ key: candidateKey, paths: new Set(found) });
    });
    return () => {
      cancelled = true;
    };
    // Inputs are serialized into candidateKey so a new array identity does not re-probe.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [supported, candidateKey, isLocalRuntime]);

  // Never expose results from a previous run/daemon, even for identical paths.
  return supported && isLocalRuntime && resolved?.key === candidateKey ? resolved.paths : new Set();
}

export function useOpenLocalFileAction(): (
  path: string,
  openerId: string,
) => Promise<OpenLocalFileResult> {
  return useCallback((path, openerId) => openLocalFile(path, openerId), []);
}
