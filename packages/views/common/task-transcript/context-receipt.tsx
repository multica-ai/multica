"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { taskContextReceiptOptions } from "@multica/core/agents";
import type {
  ContextReceipt,
  ContextReceiptSource,
} from "@multica/core/types/agent";
import { Layers, TriangleAlert } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { providerDisplayName } from "@multica/core/runtimes";
import { useT } from "../../i18n";
import { formatBytes } from "../format-bytes";

// Per-run context receipt (MUL-9013): what the daemon handed this run, where
// each piece went and how sure we are the runtime loaded it. The daemon only
// reports metadata, so this view has sizes, short hashes and paths — never
// the content itself.

type LoadTone = "ok" | "warn" | "muted";

export function loadTone(load: string): LoadTone {
  switch (load) {
    case "delivered":
    case "verified":
      return "ok";
    case "verified_other_version":
    case "unverified":
    case "not_delivered":
      return "warn";
    default:
      return "muted";
  }
}

/** Top-level rows: sources that stand on their own, not sections of another. */
export function topLevelSources(receipt: ContextReceipt): ContextReceiptSource[] {
  return receipt.sources.filter((s) => !s.parent);
}

/** Rows the reader should look at first: delivery we cannot vouch for. */
export function receiptWarnings(receipt: ContextReceipt): ContextReceiptSource[] {
  return topLevelSources(receipt).filter((s) => loadTone(s.load) === "warn");
}

export function ContextReceiptButton({ taskId, open }: { taskId: string; open: boolean }) {
  const wsId = useWorkspaceId();
  const { t } = useT("agents");
  const { data } = useQuery({
    ...taskContextReceiptOptions(wsId, taskId),
    enabled: open && !!wsId && !!taskId,
  });
  const receipt = data?.receipt;
  // Runs from a daemon that predates receipts have none; no dead button.
  if (!receipt) return null;
  const warnings = receiptWarnings(receipt);

  return (
    <Popover>
      <PopoverTrigger
        render={
          <IconButton
            variant="ghost"
            size="icon-sm"
            label={t(($) => $.transcript.context_receipt_title)}
            tooltipSide="bottom"
            className={cn("text-muted-foreground", warnings.length > 0 && "text-warning")}
          />
        }
      >
        <Layers className="h-3.5 w-3.5" />
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className="w-[36rem] max-w-[calc(100vw-2rem)] max-h-[min(36rem,calc(100vh-8rem))] overflow-y-auto p-3"
      >
        <ContextReceiptView receipt={receipt} />
      </PopoverContent>
    </Popover>
  );
}

export function ContextReceiptView({ receipt }: { receipt: ContextReceipt }) {
  const { t } = useT("agents");
  const topLevel = topLevelSources(receipt);
  const totalBytes = topLevel.reduce((sum, s) => sum + s.bytes, 0);
  const warnings = receiptWarnings(receipt);

  const runtimeLabel = [providerDisplayName(receipt.runtime), receipt.runtime_version]
    .filter(Boolean)
    .join(" ");

  return (
    <div className="space-y-3 text-caption" data-testid="context-receipt">
      <div>
        <div className="font-medium text-foreground">{t(($) => $.transcript.context_receipt_title)}</div>
        <div className="text-muted-foreground">
          {t(($) => $.transcript.context_receipt_summary, {
            count: topLevel.length,
            size: formatBytes(totalBytes),
          })}
        </div>
      </div>

      <div className="grid grid-cols-[6rem_minmax(0,1fr)] gap-x-3 gap-y-1">
        <span className="text-muted-foreground">{t(($) => $.transcript.details_runtime)}</span>
        <span className="text-foreground">{runtimeLabel || "—"}</span>
        {receipt.model && (
          <>
            <span className="text-muted-foreground">{t(($) => $.transcript.context_receipt_model)}</span>
            <span className="font-mono text-foreground">{receipt.model}</span>
          </>
        )}
        <span className="text-muted-foreground">{t(($) => $.transcript.context_receipt_session)}</span>
        <span className="text-foreground">
          {receipt.resumed_session
            ? t(($) => $.transcript.context_receipt_session_resumed)
            : receipt.fresh_retry
              ? t(($) => $.transcript.context_receipt_session_fresh_retry)
              : t(($) => $.transcript.context_receipt_session_new)}
        </span>
        <span className="text-muted-foreground">{t(($) => $.transcript.context_receipt_mcp)}</span>
        <span className="text-foreground">
          {receipt.mcp_servers.length > 0
            ? receipt.mcp_servers.join(", ")
            : t(($) => $.transcript.context_receipt_mcp_none)}
        </span>
      </div>

      {warnings.length > 0 && (
        <div className="space-y-1 rounded-md border border-warning/30 bg-warning/10 px-2.5 py-2">
          {warnings.map((s, i) => (
            <div key={`${s.kind}-${s.name ?? ""}-${i}`} className="flex items-start gap-1.5 text-foreground">
              <TriangleAlert className="mt-0.5 h-3 w-3 shrink-0 text-warning" />
              <span>
                {t(($) => $.transcript.context_receipt_warning, {
                  source: sourceLabel(s, t),
                  status: loadLabel(s, t),
                })}
              </span>
            </div>
          ))}
        </div>
      )}

      <table className="w-full table-fixed border-collapse">
        <thead>
          <tr className="border-b text-left text-muted-foreground">
            <th className="w-[34%] py-1 pr-2 font-normal">{t(($) => $.transcript.context_receipt_col_source)}</th>
            <th className="w-[12%] py-1 pr-2 text-right font-normal">{t(($) => $.transcript.context_receipt_col_size)}</th>
            <th className="w-[30%] py-1 pr-2 font-normal">{t(($) => $.transcript.context_receipt_col_delivery)}</th>
            <th className="py-1 font-normal">{t(($) => $.transcript.context_receipt_col_load)}</th>
          </tr>
        </thead>
        <tbody>
          {receipt.sources.map((s, i) => (
            <tr key={`${s.kind}-${s.name ?? ""}-${i}`} className="border-b align-top last:border-b-0">
              <td className={cn("py-1 pr-2 text-foreground", s.parent && "pl-3 text-muted-foreground")}>
                <div className="truncate" title={s.sha256 ? `sha256 ${s.sha256}` : undefined}>
                  {s.parent ? "└ " : ""}
                  {sourceLabel(s, t)}
                </div>
                {s.sha256 && <div className="font-mono text-micro text-faint-foreground">{s.sha256}</div>}
              </td>
              <td className="py-1 pr-2 text-right tabular-nums text-foreground">{formatBytes(s.bytes)}</td>
              <td className="py-1 pr-2">
                <div
                  className={cn("truncate", s.delivery === "file" ? "font-mono text-foreground" : "text-muted-foreground")}
                  title={s.path}
                >
                  {deliveryLabel(s, t)}
                </div>
              </td>
              <td className="py-1">
                {s.parent ? (
                  <span className="text-faint-foreground">{t(($) => $.transcript.context_receipt_same_as_brief)}</span>
                ) : (
                  <span className="flex items-center gap-1.5">
                    <span
                      aria-hidden
                      className={cn(
                        "inline-block h-1.5 w-1.5 shrink-0 rounded-full",
                        loadTone(s.load) === "ok" && "bg-success",
                        loadTone(s.load) === "warn" && "bg-warning",
                        loadTone(s.load) === "muted" && "bg-muted-foreground",
                      )}
                    />
                    <span className="text-foreground">{loadLabel(s, t)}</span>
                  </span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <p className="text-micro text-faint-foreground">{t(($) => $.transcript.context_receipt_footnote)}</p>
    </div>
  );
}

type T = ReturnType<typeof useT<"agents">>["t"];

function sourceLabel(source: ContextReceiptSource, t: T): string {
  switch (source.kind) {
    case "runtime_brief":
      return t(($) => $.transcript.context_source_runtime_brief);
    case "agent_instructions":
      return t(($) => $.transcript.context_source_agent_instructions);
    case "project_description":
      return t(($) => $.transcript.context_source_project_description);
    case "existing_instructions":
      return t(($) => $.transcript.context_source_existing_instructions);
    case "task_prompt":
      return t(($) => $.transcript.context_source_task_prompt);
    case "skill":
      return t(($) => $.transcript.context_source_skill, { name: source.name ?? "" });
    default:
      return source.name ? `${source.kind}: ${source.name}` : source.kind;
  }
}

function deliveryLabel(source: ContextReceiptSource, t: T): string {
  switch (source.delivery) {
    case "file":
      return source.path ?? t(($) => $.transcript.context_delivery_file);
    case "inline":
      return t(($) => $.transcript.context_delivery_inline);
    case "prompt":
      return t(($) => $.transcript.context_delivery_prompt);
    case "embedded":
      return t(($) => $.transcript.context_delivery_embedded);
    case "none":
      return t(($) => $.transcript.context_delivery_none);
    default:
      return source.delivery;
  }
}

function loadLabel(source: ContextReceiptSource, t: T): string {
  switch (source.load) {
    case "delivered":
      return t(($) => $.transcript.context_load_delivered);
    case "verified":
      return t(($) => $.transcript.context_load_verified);
    case "verified_other_version":
      return source.verified_version
        ? t(($) => $.transcript.context_load_verified_other_version, { version: source.verified_version })
        : t(($) => $.transcript.context_load_unverified);
    case "unverified":
      return t(($) => $.transcript.context_load_unverified);
    case "not_delivered":
      return t(($) => $.transcript.context_load_not_delivered);
    default:
      return source.load;
  }
}
