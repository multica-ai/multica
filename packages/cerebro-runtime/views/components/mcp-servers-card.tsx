// Runtime-level MCP servers card (9031). Lets a workspace owner/admin
// declare MCP servers once on a runtime; the daemon merges these defaults
// with each agent's mcp_config at task-claim time so every agent on the
// runtime gets the server without per-agent configuration.

"use client";

// Pulls in cerebro-types so RuntimeDevice is augmented with tools_config.
import "@multica/cerebro-types";

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { AgentRuntime } from "@multica/core/types/agent";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";

interface MCPServersCardProps {
  runtime: AgentRuntime;
  workspaceId: string;
  /** Workspace role of the viewer. Edit is restricted to owner/admin. */
  canEdit: boolean;
}

const PLACEHOLDER = JSON.stringify(
  {
    mcpServers: {
      browser: {
        command: "npx",
        args: ["@railsblueprint/blueprint-mcp@latest"],
      },
    },
  },
  null,
  2,
);

/**
 * Card with a JSON editor for the runtime's tools_config (Claude Code
 * --mcp-config format). Saves call `PATCH /api/runtimes/<id>/tools-config`.
 */
export function MCPServersCard({ runtime, workspaceId, canEdit }: MCPServersCardProps) {
  const initialJSON = useMemo(() => {
    if (!runtime.tools_config) return "";
    try {
      return JSON.stringify(runtime.tools_config, null, 2);
    } catch {
      return "";
    }
  }, [runtime.tools_config]);

  const [draft, setDraft] = useState(initialJSON);
  const [error, setError] = useState<string | null>(null);

  // Resync when server-side value changes (e.g. another tab saved).
  useEffect(() => {
    setDraft(initialJSON);
    setError(null);
  }, [initialJSON]);

  const qc = useQueryClient();
  const mutation = useMutation<AgentRuntime, Error, { value: unknown | null }>({
    mutationFn: ({ value }) => api.updateRuntimeToolsConfig(runtime.id, value),
    onSuccess: () => {
      // Invalidate both list keys so any view of this runtime (list, "mine",
      // detail-driven-by-list-find) sees the new tools_config on next render.
      qc.invalidateQueries({ queryKey: runtimeKeys.all(workspaceId) });
      toast.success("MCP-config gemt");
    },
    onError: (err) => {
      toast.error(err.message || "Kunne ikke gemme MCP-config");
    },
  });

  const dirty = draft.trim() !== initialJSON.trim();
  const isEmpty = draft.trim() === "";

  const handleSave = () => {
    setError(null);
    if (isEmpty) {
      mutation.mutate({ value: null });
      return;
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(draft);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Ugyldig JSON");
      return;
    }
    mutation.mutate({ value: parsed });
  };

  const handleClear = () => {
    setDraft("");
    setError(null);
    mutation.mutate({ value: null });
  };

  return (
    <div className="rounded-md border bg-card p-4">
      <div className="mb-2 flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium">MCP servers</h3>
          <p className="text-xs text-muted-foreground">
            Runtime-default MCPs der merges ind i alle agents på denne runtime.
          </p>
        </div>
        {runtime.tools_config != null && (
          <span className="rounded bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-700">
            Aktiv
          </span>
        )}
      </div>

      <Textarea
        value={draft}
        onChange={(e) => {
          setDraft(e.target.value);
          if (error) setError(null);
        }}
        placeholder={canEdit ? PLACEHOLDER : "Ingen runtime-default MCPs konfigureret."}
        disabled={!canEdit || mutation.isPending}
        spellCheck={false}
        rows={10}
        className="font-mono text-xs"
      />

      {error && (
        <p className="mt-2 text-xs text-destructive">
          JSON-fejl: {error}
        </p>
      )}

      {canEdit && (
        <div className="mt-3 flex items-center justify-between">
          <p className="text-xs text-muted-foreground">
            Format: <code className="text-xs">{`{"mcpServers": {...}}`}</code>.
            Agent-niveau <code className="text-xs">mcp_config</code> overrider pr. server-nøgle.
          </p>
          <div className="flex gap-2">
            {runtime.tools_config != null && !dirty && (
              <Button
                variant="outline"
                size="sm"
                onClick={handleClear}
                disabled={mutation.isPending}
              >
                Ryd
              </Button>
            )}
            <Button
              size="sm"
              onClick={handleSave}
              disabled={!dirty || mutation.isPending}
            >
              {mutation.isPending ? "Gemmer…" : "Gem"}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
