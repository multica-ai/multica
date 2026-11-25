import type {
  RunFileChange,
  RunFileChanges,
} from "@multica/core/agents/file-changes";
import {
  joinRunRootRelativePath,
  resolveRunFileChangeCandidate,
  RUN_ROOT_SELF_LABEL,
} from "../../platform/open-local-file";

export type RunFileChangeEntry = {
  file: RunFileChange;
  page: RunFileChanges;
};

export type RunFileChangeRoot = {
  id: string;
  label: string;
  kind: string;
  coverage: string;
};

export type RunFileChangeGroup = {
  rootId: string;
  displayName: string;
  label: string;
  kind: string;
  coverage: string;
  partial: boolean;
  files: RunFileChangeEntry[];
};

const KNOWN_ROOT_KINDS = new Set(["git", "directory"]);

export function isRunWorkdirRoot(label: string): boolean {
  return label === RUN_ROOT_SELF_LABEL;
}

export function isPartialCollectedRoot(root: {
  kind: string;
  coverage: string;
}): boolean {
  return root.coverage !== "complete" || !KNOWN_ROOT_KINDS.has(root.kind);
}

export function rootName(
  id: string,
  roots: readonly { id: string; label: string }[],
): string {
  const label = roots.find((root) => root.id === id)?.label;
  if (!label) return id;
  return roots.filter((root) => root.label === label).length > 1
    ? `${label} (${id})`
    : label;
}

export function copyRunFileChangePath(
  rootLabel: string,
  filePath: string,
  workdir?: string,
): string {
  return (
    resolveRunFileChangeCandidate(rootLabel, filePath, workdir) ??
    joinRunRootRelativePath(rootLabel, filePath) ??
    filePath
  );
}

function rootFromPages(
  rootId: string,
  files: readonly RunFileChangeEntry[],
): RunFileChangeRoot | undefined {
  for (const { page } of files) {
    const root = page.roots.find((candidate) => candidate.id === rootId);
    if (root) return root;
  }
  return undefined;
}

function groupIsPartial(
  files: readonly RunFileChangeEntry[],
  orderedRoot: RunFileChangeRoot | undefined,
): boolean {
  if (orderedRoot && isPartialCollectedRoot(orderedRoot)) return true;
  return files.some(({ file, page }) => {
    const pageRoot = page.roots.find((candidate) => candidate.id === file.rootId);
    return !pageRoot || isPartialCollectedRoot(pageRoot);
  });
}

/**
 * Group persisted run file changes by root. Project roots keep
 * `collection.roots` order and the workdir sentinel always sorts last.
 * Roots with no loaded files are omitted.
 */
export function groupRunFileChanges(
  files: readonly RunFileChangeEntry[],
  roots: readonly RunFileChangeRoot[],
): RunFileChangeGroup[] {
  const buckets = new Map<string, RunFileChangeEntry[]>();
  for (const entry of files) {
    const list = buckets.get(entry.file.rootId);
    if (list) list.push(entry);
    else buckets.set(entry.file.rootId, [entry]);
  }

  const extraRoots: RunFileChangeRoot[] = [];
  for (const [rootId, entries] of buckets) {
    if (roots.some((root) => root.id === rootId)) continue;
    extraRoots.push(
      rootFromPages(rootId, entries) ?? {
        id: rootId,
        label: rootId,
        kind: "",
        coverage: "",
      },
    );
  }
  const knownRoots = [...roots, ...extraRoots];

  const groups: RunFileChangeGroup[] = [];
  for (const [rootId, groupFiles] of buckets) {
    const fromOrder = knownRoots.find((root) => root.id === rootId);
    const fromPage = rootFromPages(rootId, groupFiles);
    const root = fromPage ??
      fromOrder ?? {
        id: rootId,
        label: rootId,
        kind: "",
        coverage: "",
      };
    groups.push({
      rootId,
      displayName: rootName(rootId, knownRoots),
      label: root.label,
      kind: root.kind,
      coverage: root.coverage,
      partial: groupIsPartial(groupFiles, fromOrder),
      files: groupFiles,
    });
  }

  groups.sort((a, b) => {
    const aw = isRunWorkdirRoot(a.label) ? 1 : 0;
    const bw = isRunWorkdirRoot(b.label) ? 1 : 0;
    if (aw !== bw) return aw - bw;
    const ai = knownRoots.findIndex((root) => root.id === a.rootId);
    const bi = knownRoots.findIndex((root) => root.id === b.rootId);
    const aIndex = ai === -1 ? Number.MAX_SAFE_INTEGER : ai;
    const bIndex = bi === -1 ? Number.MAX_SAFE_INTEGER : bi;
    return aIndex - bIndex;
  });

  return groups;
}
