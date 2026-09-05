"use client";

import { useId, type ReactNode } from "react";
import {
  Check,
  Globe2,
  Loader2,
  Pencil,
  Server,
  SlidersHorizontal,
  SquareTerminal,
  Trash2,
  X,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { mcpTransportLabel } from "./mcp-transport";

export type McpServerRowLabels = {
  rename: string;
  renameAria: string;
  renameSave: string;
  renameCancel: string;
  configure: string;
  configureAria: string;
  remove: string;
  removeAria: string;
};

export type McpServerRenameState = {
  draft: string;
  error: string;
  pending: boolean;
  onChange: (value: string) => void;
  onCancel: () => void;
  onSubmit: () => void;
};

export function McpTransportIcon({ transport }: { transport: string }) {
  const normalizedTransport = transport.trim().toLowerCase();
  const transportLabel = mcpTransportLabel(transport);
  const TransportIcon =
    normalizedTransport === "stdio" || normalizedTransport === "local"
      ? SquareTerminal
      : normalizedTransport === "http" ||
          normalizedTransport === "remote" ||
          normalizedTransport === "streamable-http"
        ? Globe2
        : Server;

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            tabIndex={0}
            role="img"
            aria-label={transportLabel}
            className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-surface-border bg-muted/40 text-muted-foreground outline-none transition-colors hover:bg-muted focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <TransportIcon className="size-4" aria-hidden="true" />
          </span>
        }
      />
      <TooltipContent>{transportLabel}</TooltipContent>
    </Tooltip>
  );
}

/** One destructive row action, with identical resting and hover treatment. */
export function McpRemoveButton({
  ariaLabel,
  tooltipLabel,
  disabled = false,
  onClick,
}: {
  ariaLabel: string;
  tooltipLabel: string;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
            onClick={onClick}
            disabled={disabled}
            aria-label={ariaLabel}
          >
            <Trash2 />
          </Button>
        }
      />
      <TooltipContent>{tooltipLabel}</TooltipContent>
    </Tooltip>
  );
}

/**
 * Shared MCP inventory row used wherever Multica owns the server entry.
 *
 * Data writes remain the caller's responsibility: workspace settings replaces
 * a write-only config, while an agent edits its readable local config. Keeping
 * those semantics outside this component prevents the visual treatment and
 * the security contract from becoming coupled again. Both open the same
 * configuration dialog, so both carry the same "configure" icon; only the
 * caller's tooltip and accessible name say whether it edits or replaces.
 */
export function McpServerRow({
  name,
  transport,
  status,
  canManage,
  actionsDisabled = false,
  rename,
  labels,
  onRenameStart,
  onConfigure,
  onRemove,
}: {
  name: string;
  transport: string;
  status?: ReactNode;
  canManage: boolean;
  actionsDisabled?: boolean;
  rename?: McpServerRenameState;
  labels: McpServerRowLabels;
  onRenameStart: () => void;
  onConfigure: () => void;
  onRemove: () => void;
}) {
  const errorId = `${useId()}-rename-error`;

  return (
    <li className="group flex min-h-16 items-center gap-3 px-4 py-3 transition-colors hover:bg-muted/30">
      <McpTransportIcon transport={transport} />

      <div className="min-w-0 flex-1 has-[form]:space-y-2">
        <div className="flex min-w-0 items-center gap-2">
          {rename ? (
            <form
              className="flex min-w-0 flex-1 items-start gap-2"
              onSubmit={(event) => {
                event.preventDefault();
                rename.onSubmit();
              }}
            >
              <div className="min-w-0 flex-1 sm:max-w-96">
                <Input
                  autoFocus
                  className="bg-background"
                  value={rename.draft}
                  onChange={(event) => rename.onChange(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Escape") {
                      event.preventDefault();
                      rename.onCancel();
                    }
                  }}
                  aria-label={labels.renameAria}
                  aria-invalid={rename.error ? true : undefined}
                  aria-describedby={rename.error ? errorId : undefined}
                  disabled={rename.pending}
                />
                {rename.error ? (
                  <p
                    id={errorId}
                    role="alert"
                    className="mt-1 text-caption text-destructive"
                  >
                    {rename.error}
                  </p>
                ) : null}
              </div>
              <Button
                type="submit"
                variant="secondary"
                size="icon-sm"
                className="mt-0.5"
                disabled={rename.pending}
                aria-label={labels.renameSave}
              >
                {rename.pending ? (
                  <Loader2 className="animate-spin motion-reduce:animate-none" />
                ) : (
                  <Check />
                )}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                className="mt-0.5 text-muted-foreground"
                disabled={rename.pending}
                onClick={rename.onCancel}
                aria-label={labels.renameCancel}
              >
                <X />
              </Button>
            </form>
          ) : (
            <span className="truncate text-body font-medium">{name}</span>
          )}
          {status}
        </div>
        <p className="text-caption text-muted-foreground">
          {mcpTransportLabel(transport)}
        </p>
      </div>

      {canManage && !rename ? (
        <div className="flex shrink-0 items-center gap-0.5 text-muted-foreground">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  onClick={onRenameStart}
                  disabled={actionsDisabled}
                  aria-label={`${labels.renameAria} ${name}`}
                >
                  <Pencil />
                </Button>
              }
            />
            <TooltipContent>{labels.rename}</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  onClick={onConfigure}
                  disabled={actionsDisabled}
                  aria-label={`${labels.configureAria} ${name}`}
                >
                  <SlidersHorizontal />
                </Button>
              }
            />
            <TooltipContent>{labels.configure}</TooltipContent>
          </Tooltip>
          <div className="mx-1 h-4 w-px bg-surface-border" aria-hidden="true" />
          <McpRemoveButton
            ariaLabel={`${labels.removeAria} ${name}`}
            tooltipLabel={labels.remove}
            disabled={actionsDisabled}
            onClick={onRemove}
          />
        </div>
      ) : null}
    </li>
  );
}
