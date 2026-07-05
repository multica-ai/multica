"use client";

// CEREBRO-PATCH(agent-office-skills-mcp-versioned): FIR-1775 2b — versioned Skills editing.
// Agent Office (FIR-1775) — the Skills tab edits an agent's bound skills through
// the SAME versioned change-request flow as Instructions, instead of writing the
// binding directly. You build a draft (add / remove), then Propose change; the
// owner/approvers review the diff and approve, which applies it and snapshots a
// new version. Pending proposals and version history (scoped to skills) render
// below so propose → approve → diff → restore all live on this tab.

import { useEffect, useMemo, useState } from "react";
import { FileText, Plus, Send, Trash2, Undo2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  memberListOptions,
  skillListOptions,
} from "@multica/core/workspace/queries";
import {
  AgentContextChangeRequestQueue,
  AgentContextFieldProposeDialog,
  AgentContextVersionsPanel,
} from "@multica/cerebro-agent-context/views";
import { Button } from "@multica/ui/components/ui/button";
import { SkillAddDialog } from "../skill-add-dialog";
import { useT } from "../../../i18n";

// Only the skills field is in scope for this tab's diffs and proposal queue.
const SKILLS_KEYS = ["skill_ids"];

export function SkillsTab({
  agent,
  canEdit = true,
}: {
  agent: Agent;
  canEdit?: boolean;
}) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const isOwner = !!(userId && agent.owner_id && agent.owner_id === userId);
  const canManage = canEdit || isOwner;

  // The live bound set is the base for the draft; proposing diffs the draft
  // against the agent's current versioned snapshot.
  const currentIds = useMemo(
    () => agent.skills.map((s) => s.id),
    [agent.skills],
  );
  const [draftIds, setDraftIds] = useState<string[]>(currentIds);
  const [showAdd, setShowAdd] = useState(false);
  const [showPropose, setShowPropose] = useState(false);

  // Re-sync the draft to the live set whenever it changes underneath us (e.g.
  // a proposal merged elsewhere) and the user has no pending local edits.
  useEffect(() => {
    setDraftIds((cur) =>
      sameSet(cur, currentIds) ? currentIds : cur,
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentIds.join("|")]);

  // Resolve a draft id to a display name/description from the live agent skills
  // first, falling back to the workspace skill catalogue for freshly added ones.
  const resolve = (id: string) => {
    const onAgent = agent.skills.find((s) => s.id === id);
    if (onAgent) return { name: onAgent.name, description: onAgent.description };
    const ws = workspaceSkills.find((s) => s.id === id);
    return { name: ws?.name ?? id, description: ws?.description ?? "" };
  };

  const dirty = !sameSet(draftIds, currentIds);

  const handleRemove = (id: string) =>
    setDraftIds((cur) => cur.filter((x) => x !== id));
  const handleAdd = (ids: string[]) =>
    setDraftIds((cur) => Array.from(new Set([...cur, ...ids])));
  const handleDiscard = () => setDraftIds(currentIds);

  return (
    <div className="space-y-5">
      <div className="flex items-start justify-between gap-3">
        <div className="max-w-prose space-y-1">
          <p className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.skills.intro)}
          </p>
          <p className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.skills.versioned_note)}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowAdd(true)}
            disabled={!canManage || workspaceSkills.length === 0}
          >
            <Plus className="h-3 w-3" />
            {t(($) => $.tab_body.skills.add_action)}
          </Button>
        </div>
      </div>

      {draftIds.length === 0 ? (
        <div className="flex flex-col items-center justify-center rounded-lg border border-dashed py-10">
          <FileText className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">
            {t(($) => $.tab_body.skills.empty_title)}
          </p>
          <p className="mt-1 max-w-xs text-center text-xs text-muted-foreground">
            {t(($) => $.tab_body.skills.empty_hint)}
          </p>
        </div>
      ) : (
        <ul className="space-y-1.5">
          {draftIds.map((id) => {
            const { name, description } = resolve(id);
            const added = !currentIds.includes(id);
            return (
              <li
                key={id}
                className={`flex items-center gap-2.5 rounded-md border px-3 py-2 ${
                  added ? "border-emerald-500/40 bg-emerald-500/5" : ""
                }`}
              >
                <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5 text-sm font-medium">
                    {name}
                    {added && (
                      <span className="rounded-full bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-700 dark:text-emerald-400">
                        {t(($) => $.tab_body.skills.added_draft_badge)}
                      </span>
                    )}
                  </div>
                  {description && (
                    <div className="truncate text-xs text-muted-foreground">
                      {description}
                    </div>
                  )}
                </div>
                {canManage && (
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => handleRemove(id)}
                    className="text-muted-foreground hover:text-destructive"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}

      {dirty && canManage && (
        <div className="flex items-center justify-between gap-3 rounded-md border bg-muted/30 px-3 py-2">
          <span className="text-xs text-muted-foreground">
            {t(($) => $.tab_body.skills.draft_hint)}
          </span>
          <div className="flex items-center gap-1.5">
            <Button variant="ghost" size="sm" onClick={handleDiscard}>
              <Undo2 className="h-3 w-3" />
              {t(($) => $.tab_body.common.discard)}
            </Button>
            <Button
              size="sm"
              onClick={() => setShowPropose(true)}
              className="bg-blue-600 text-white hover:bg-blue-700"
            >
              <Send className="h-3 w-3" />
              {t(($) => $.tab_body.common.propose_change)}
            </Button>
          </div>
        </div>
      )}

      <div className="my-1 h-px bg-border" />

      <div className="space-y-5">
        <AgentContextChangeRequestQueue
          agent={agent}
          wsId={wsId}
          members={members}
          canReview={canManage}
          onlyKeys={SKILLS_KEYS}
          title="Skill change requests"
        />
        <AgentContextVersionsPanel
          agent={agent}
          wsId={wsId}
          members={members}
          canManage={canManage}
          onlyKeys={SKILLS_KEYS}
          title="Skill version history"
        />
      </div>

      {canManage && (
        <SkillAddDialog
          agent={agent}
          open={showAdd}
          onOpenChange={setShowAdd}
          attachedIds={draftIds}
          onAdd={handleAdd}
        />
      )}

      <AgentContextFieldProposeDialog
        agent={agent}
        open={showPropose}
        onOpenChange={setShowPropose}
        override={{ skill_ids: draftIds }}
        fieldLabel="skills"
        defaultTitle="Update bound skills"
      />
    </div>
  );
}

// Order-insensitive set comparison for skill id lists.
function sameSet(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const sb = new Set(b);
  return a.every((x) => sb.has(x));
}
