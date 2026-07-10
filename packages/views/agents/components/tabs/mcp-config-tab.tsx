"use client";

// CEREBRO-PATCH(agent-office-skills-mcp-versioned): FIR-1775 2b — versioned MCP config editing.
// Agent Office (FIR-1775) — the MCP config tab edits an agent's mcp_config
// through the SAME versioned change-request flow as Instructions, instead of
// writing the agent row directly. You edit a draft, then Propose change; the
// owner/approvers review the diff and approve, which applies it and snapshots a
// new version. Pending proposals and version history (scoped to mcp_config)
// render below so propose → approve → diff → restore all live on this tab.

import { useEffect, useMemo, useRef, useState } from "react";
import { Eraser, Lock, Send } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useQuery } from "@tanstack/react-query";
import {
  AgentContextChangeRequestQueue,
  AgentContextFieldProposeDialog,
  AgentContextVersionsPanel,
} from "@multica/cerebro-agent-context/views";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../../i18n";

// Only the mcp_config field is in scope for this tab's diffs and proposal queue.
const MCP_KEYS = ["mcp_config"];

// `null` and the empty string both mean "no config" — normalise to the empty
// string in the editor so the dirty check has one canonical form to compare.
function configToText(value: unknown): string {
  if (value === null || value === undefined) return "";
  return JSON.stringify(value, null, 2);
}

export function McpConfigTab({
  agent,
  canEdit = true,
  onDirtyChange,
}: {
  agent: Agent;
  canEdit?: boolean;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const isOwner = !!(userId && agent.owner_id && agent.owner_id === userId);
  const canManage = canEdit || isOwner;

  const redacted = agent.mcp_config_redacted === true;
  const original = useMemo(() => configToText(agent.mcp_config), [agent.mcp_config]);
  const [text, setText] = useState(original);
  const [showPropose, setShowPropose] = useState(false);

  // Sync the draft when the agent prop changes (e.g. a proposal merged) only
  // when the user has no in-flight edits — comparing against the *previous*
  // original is what tells us "untouched since the last sync".
  const previousOriginalRef = useRef(original);
  useEffect(() => {
    setText((current) =>
      current === previousOriginalRef.current ? original : current,
    );
    previousOriginalRef.current = original;
  }, [original]);

  const trimmed = text.trim();
  const parseResult = useMemo<
    | { ok: true; value: unknown | null }
    | { ok: false; error: string }
  >(() => {
    if (trimmed === "") return { ok: true, value: null };
    try {
      const value = JSON.parse(trimmed);
      // The MCP CLI accepts an object (`{"mcpServers": …}`); a top-level array
      // or primitive is almost certainly a mistake, so reject it here.
      if (value === null || typeof value !== "object" || Array.isArray(value)) {
        return { ok: false, error: "mcp_config_not_object" };
      }
      return { ok: true, value };
    } catch (err) {
      return {
        ok: false,
        error: err instanceof Error ? err.message : "invalid JSON",
      };
    }
  }, [trimmed]);

  const dirty = text !== original;

  useEffect(() => {
    onDirtyChange?.(dirty);
  }, [dirty, onDirtyChange]);

  if (redacted) {
    return (
      <div className="space-y-3">
        <p className="flex items-center gap-2 text-sm font-medium">
          <Lock className="h-3.5 w-3.5 text-muted-foreground" />
          {t(($) => $.tab_body.mcp_config.redacted_title)}
        </p>
        <p className="text-xs text-muted-foreground">
          {t(($) => $.tab_body.mcp_config.redacted_hint)}
        </p>
      </div>
    );
  }

  const handleClear = () => setText("");

  const showInvalid = trimmed !== "" && !parseResult.ok;
  const invalidMessage =
    !parseResult.ok && parseResult.error === "mcp_config_not_object"
      ? t(($) => $.tab_body.mcp_config.invalid_not_object)
      : !parseResult.ok
        ? t(($) => $.tab_body.mcp_config.invalid_json, { error: parseResult.error })
        : "";

  return (
    <div className="flex h-full flex-col space-y-5">
      <div className="flex items-start justify-between gap-3">
        <div className="max-w-prose space-y-1">
          <p className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.mcp_config.intro)}
          </p>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.mcp_config.versioned_note)}
          </p>
        </div>
        {trimmed !== "" && canManage && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleClear}
            className="shrink-0"
          >
            <Eraser className="h-3 w-3" />
            {t(($) => $.tab_body.mcp_config.clear_action)}
          </Button>
        )}
      </div>

      <Textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={t(($) => $.tab_body.mcp_config.placeholder)}
        aria-invalid={showInvalid || undefined}
        aria-label={t(($) => $.tab_body.mcp_config.editor_aria)}
        spellCheck={false}
        readOnly={!canManage}
        className="min-h-[220px] font-mono text-xs"
      />

      {showInvalid && <p className="text-xs text-destructive">{invalidMessage}</p>}

      {canManage && (
        <div className="flex items-center justify-end gap-3">
          {dirty && (
            <span className="text-xs text-muted-foreground">
              {t(($) => $.tab_body.common.unsaved_changes)}
            </span>
          )}
          <Button
            onClick={() => setShowPropose(true)}
            disabled={!dirty || !parseResult.ok}
            size="sm"
            className="bg-blue-600 text-white hover:bg-blue-700"
          >
            <Send className="h-3.5 w-3.5" />
            {t(($) => $.tab_body.common.propose_change)}
          </Button>
        </div>
      )}

      <div className="my-1 h-px bg-border" />

      <div className="space-y-5">
        <AgentContextChangeRequestQueue
          agent={agent}
          wsId={wsId}
          members={members}
          canReview={canManage}
          onlyKeys={MCP_KEYS}
          title="MCP config change requests"
        />
        <AgentContextVersionsPanel
          agent={agent}
          wsId={wsId}
          members={members}
          canManage={canManage}
          onlyKeys={MCP_KEYS}
          title="MCP config version history"
        />
      </div>

      <AgentContextFieldProposeDialog
        agent={agent}
        open={showPropose}
        onOpenChange={setShowPropose}
        override={{ mcp_config: parseResult.ok ? parseResult.value : undefined }}
        fieldLabel="MCP config"
        defaultTitle="Update MCP config"
      />
    </div>
  );
}
