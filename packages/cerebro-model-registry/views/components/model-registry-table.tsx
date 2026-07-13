"use client";

import type { ModelRegistrySnapshot } from "@multica/core/types";
import { ModelRegistryProposeDialog } from "./model-registry-propose-dialog";

interface Props {
  snapshot: ModelRegistrySnapshot;
  currentVersion: string;
  canReview: boolean;
}

function formatUsd(n: number): string {
  return n === 0 ? "$0" : `$${n}`;
}

function formatContextWindow(n: number): string {
  if (n <= 0) return "—";
  if (n >= 1_000_000) return `${n / 1_000_000}M`;
  if (n >= 1_000) return `${n / 1_000}K`;
  return String(n);
}

function formatCacheLifetime(seconds?: number): string {
  if (seconds == null || seconds <= 0) return "—";
  if (seconds % 3600 === 0) return `${seconds / 3600}h`;
  if (seconds % 60 === 0) return `${seconds / 60}m`;
  return `${seconds}s`;
}

export function ModelRegistryTable({ snapshot, currentVersion, canReview }: Props) {
  const rows = Object.entries(snapshot.models).sort(([a], [b]) => a.localeCompare(b));

  return (
    <div className="overflow-x-auto rounded-md border">
      <table className="w-full text-xs">
        <thead className="bg-muted/40 text-muted-foreground">
          <tr className="text-left">
            <th className="px-2.5 py-2 font-medium">Model id</th>
            <th className="px-2.5 py-2 font-medium">Label</th>
            <th className="px-2.5 py-2 font-medium">Provider</th>
            <th className="px-2.5 py-2 font-medium">Context</th>
            <th className="px-2.5 py-2 font-medium">Cache lifetime</th>
            <th className="px-2.5 py-2 text-right font-medium">Input $/Mtok</th>
            <th className="px-2.5 py-2 text-right font-medium">Output $/Mtok</th>
            <th className="px-2.5 py-2 text-right font-medium">Cache read</th>
            <th className="px-2.5 py-2 text-right font-medium">Cache write</th>
            <th className="px-2.5 py-2" />
          </tr>
        </thead>
        <tbody>
          {rows.map(([id, entry]) => (
            <tr key={id} className="border-t">
              <td className="px-2.5 py-1.5 font-mono">
                {id}
                {id === snapshot.fallback_model && (
                  <span className="ml-1.5 rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                    fallback
                  </span>
                )}
              </td>
              <td className="px-2.5 py-1.5">{entry.label}</td>
              <td className="px-2.5 py-1.5 text-muted-foreground">{entry.provider}</td>
              <td className="px-2.5 py-1.5">{formatContextWindow(entry.context_window)}</td>
              <td className="px-2.5 py-1.5">{formatCacheLifetime(entry.cache_ttl_seconds)}</td>
              <td className="px-2.5 py-1.5 text-right font-mono">
                {formatUsd(entry.input_usd_per_mtok)}
              </td>
              <td className="px-2.5 py-1.5 text-right font-mono">
                {formatUsd(entry.output_usd_per_mtok)}
              </td>
              <td className="px-2.5 py-1.5 text-right font-mono">
                {formatUsd(entry.cache_read_usd_per_mtok)}
              </td>
              <td className="px-2.5 py-1.5 text-right font-mono">
                {formatUsd(entry.cache_write_usd_per_mtok)}
              </td>
              <td className="px-2.5 py-1.5 text-right">
                <ModelRegistryProposeDialog
                  currentVersion={currentVersion}
                  modelId={id}
                  entry={entry}
                  canReview={canReview}
                />
              </td>
            </tr>
          ))}
          {rows.length === 0 && (
            <tr>
              <td colSpan={10} className="px-2.5 py-4 text-center text-muted-foreground">
                No models in the registry yet.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
