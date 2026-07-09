"use client";

import { useState } from "react";
import {
  Check,
  ChevronRight,
  Loader2,
  MoreHorizontal,
  Plus,
  Search,
  Trash2,
  X,
} from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Agent, SkillSummary } from "@multica/core/types";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@multica/ui/components/ui/collapsible";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import type { SkillRow } from "./skills-page";

// Shared context the row kebab and the batch toolbar both need. Assembled
// once at the page level.
export interface SkillActionsContext {
  wsId: string;
  agents: Agent[];
  currentUserId: string | null;
  /** Workspace owner/admin — may manage every agent, not only their own. */
  isAdmin: boolean;
}

// ---------------------------------------------------------------------------
// Add-to-agent dialog (multi-select; shared by row kebab and batch toolbar)
// ---------------------------------------------------------------------------

// Attaching is permission-gated by the TARGET agent (its owner, or a
// workspace owner/admin — server/internal/handler/agent.go canManageAgent).
// Members therefore only see their own agents; admins additionally see
// everyone else's. Agents that already have every selected skill render
// checked-out and disabled; partial overlap stays selectable because the
// endpoint is additive ("ensure these skills") and idempotent.
function partitionAgents(ctx: SkillActionsContext) {
  const active = ctx.agents.filter((a) => !a.archived_at);
  const mine = active.filter(
    (a) => a.owner_id !== null && a.owner_id === ctx.currentUserId,
  );
  const others = ctx.isAdmin
    ? active.filter(
        (a) => a.owner_id === null || a.owner_id !== ctx.currentUserId,
      )
    : [];
  return { mine, others };
}

function AgentPickerRow({
  agent,
  skillIds,
  selected,
  onToggle,
}: {
  agent: Agent;
  skillIds: string[];
  selected: boolean;
  onToggle: (agent: Agent) => void;
}) {
  const { t } = useT("skills");
  const owned = skillIds.filter((id) =>
    agent.skills.some((s) => s.id === id),
  ).length;
  const hasAll = owned === skillIds.length;
  return (
    <button
      type="button"
      disabled={hasAll}
      onClick={() => onToggle(agent)}
      aria-pressed={selected}
      className={cn(
        "flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors",
        hasAll
          ? "opacity-50"
          : selected
            ? "bg-accent"
            : "hover:bg-accent/50",
      )}
    >
      {/* Indicator only — the wrapping <button> handles clicks. */}
      <Checkbox
        checked={hasAll || selected}
        tabIndex={-1}
        className="pointer-events-none"
      />
      <ActorAvatar
        name={agent.name}
        initials={agent.name.slice(0, 2).toUpperCase()}
        avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
        isAgent
        size={22}
      />
      <span className="min-w-0 flex-1 truncate text-sm">{agent.name}</span>
      {hasAll ? (
        <Check className="size-3.5 shrink-0 text-muted-foreground" />
      ) : owned > 0 ? (
        <span className="shrink-0 text-xs text-muted-foreground">
          {t(($) => $.actions.has_partial, { owned, total: skillIds.length })}
        </span>
      ) : null}
    </button>
  );
}

// Shows the first few skill names as chips so the dialog says WHAT is being
// added; the overflow collapses into a "+N" badge with the remaining names
// in a tooltip. Number-agnostic, so single-row and batch share the layout.
const MAX_SKILL_CHIPS = 3;
const MAX_TOOLTIP_NAMES = 10;

function SkillChips({ skills }: { skills: SkillSummary[] }) {
  const visible = skills.slice(0, MAX_SKILL_CHIPS);
  const overflow = skills.slice(MAX_SKILL_CHIPS);
  const chipClass =
    "max-w-[10rem] truncate rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground";
  return (
    <div className="flex flex-wrap items-center gap-1">
      {visible.map((s) => (
        <span key={s.id} className={chipClass}>
          {s.name}
        </span>
      ))}
      {overflow.length > 0 && (
        <Tooltip>
          <TooltipTrigger
            render={
              <span className={cn(chipClass, "font-medium")}>
                +{overflow.length}
              </span>
            }
          />
          <TooltipContent side="bottom" className="max-w-64">
            {overflow
              .slice(0, MAX_TOOLTIP_NAMES)
              .map((s) => s.name)
              .join(", ")}
            {overflow.length > MAX_TOOLTIP_NAMES ? "…" : ""}
          </TooltipContent>
        </Tooltip>
      )}
    </div>
  );
}

// Collapsible agent group; "my agents" opens by default, other people's
// agents start collapsed.
function AgentGroup({
  label,
  agents,
  defaultOpen,
  skillIds,
  selectedIds,
  onToggle,
}: {
  label: string;
  agents: Agent[];
  defaultOpen: boolean;
  skillIds: string[];
  selectedIds: ReadonlySet<string>;
  onToggle: (agent: Agent) => void;
}) {
  if (agents.length === 0) return null;
  return (
    <Collapsible defaultOpen={defaultOpen}>
      <CollapsibleTrigger className="group/trigger flex w-full items-center gap-1 rounded-md px-2.5 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent/50">
        <ChevronRight className="size-3 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
        <span>{label}</span>
        <span className="text-muted-foreground/60">{agents.length}</span>
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-0.5">
        {agents.map((agent) => (
          <AgentPickerRow
            key={agent.id}
            agent={agent}
            skillIds={skillIds}
            selected={selectedIds.has(agent.id)}
            onToggle={onToggle}
          />
        ))}
      </CollapsibleContent>
    </Collapsible>
  );
}

export function AddToAgentDialog({
  skills,
  ctx,
  open,
  onOpenChange,
}: {
  skills: SkillSummary[];
  ctx: SkillActionsContext;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("skills");
  const qc = useQueryClient();
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(
    new Set(),
  );
  const [saving, setSaving] = useState(false);
  const [query, setQuery] = useState("");

  const skillIds = skills.map((s) => s.id);
  const { mine, others } = partitionAgents(ctx);
  const count = selectedIds.size;

  // Search across both groups by agent name. Filtering is applied per group
  // so the "My agents" / "Other agents" split is preserved; when a query is
  // active both groups are force-expanded (via the remount key below) so
  // matches in the normally-collapsed "Other agents" group stay visible.
  const trimmedQuery = query.trim().toLowerCase();
  const searching = trimmedQuery.length > 0;
  const matchesQuery = (a: Agent) =>
    !searching || a.name.toLowerCase().includes(trimmedQuery);
  const filteredMine = mine.filter(matchesQuery);
  const filteredOthers = others.filter(matchesQuery);
  const hasAnyAgent = mine.length + others.length > 0;
  const hasMatch = filteredMine.length + filteredOthers.length > 0;

  const handleOpenChange = (v: boolean) => {
    if (saving) return;
    if (!v) {
      setSelectedIds(new Set());
      setQuery("");
    }
    onOpenChange(v);
  };

  const handleToggle = (agent: Agent) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(agent.id)) next.delete(agent.id);
      else next.add(agent.id);
      return next;
    });
  };

  const handleConfirm = async () => {
    const targets = [...mine, ...others].filter((a) => selectedIds.has(a.id));
    if (targets.length === 0) return;
    setSaving(true);
    try {
      for (const agent of targets) {
        const missing = skillIds.filter(
          (id) => !agent.skills.some((s) => s.id === id),
        );
        if (missing.length > 0) {
          await api.addAgentSkills(agent.id, { skill_ids: missing });
        }
      }
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(ctx.wsId) });
      toast.success(
        targets.length === 1 && targets[0]
          ? t(($) => $.actions.added_toast, { name: targets[0].name })
          : t(($) => $.actions.added_multi_toast, { count: targets.length }),
      );
      setSelectedIds(new Set());
      onOpenChange(false);
    } catch (e) {
      toast.error(
        e instanceof Error && e.message
          ? e.message
          : t(($) => $.actions.add_failed_toast),
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      {/* Fixed-height dialog: header/chips/footer stay put, only the agent
          list scrolls (flex-1 + min-h-0). One size up from the default
          confirm dialogs — this is a working picker, not a prompt. */}
      <DialogContent className="flex h-[32rem] max-h-[85svh] max-w-lg flex-col">
        <DialogHeader>
          <DialogTitle className="text-sm">
            {t(($) => $.actions.add_to_agent)}
          </DialogTitle>
          <DialogDescription className="text-xs">
            {t(($) => $.actions.add_dialog_description)}
          </DialogDescription>
        </DialogHeader>

        <SkillChips skills={skills} />

        {hasAnyAgent && (
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t(($) => $.actions.search_placeholder)}
              className="h-8 pl-7 text-xs"
            />
          </div>
        )}

        <div className="min-h-0 flex-1 overflow-y-auto rounded-lg border bg-card p-1.5">
          {!hasAnyAgent ? (
            <div className="py-6 text-center text-xs text-muted-foreground">
              {t(($) => $.actions.no_agents)}
            </div>
          ) : !hasMatch ? (
            <div className="py-6 text-center text-xs text-muted-foreground">
              {t(($) => $.actions.no_agents_match)}
            </div>
          ) : (
            <>
              <AgentGroup
                key={`mine-${searching}`}
                label={t(($) => $.actions.my_agents)}
                agents={filteredMine}
                defaultOpen
                skillIds={skillIds}
                selectedIds={selectedIds}
                onToggle={handleToggle}
              />
              <AgentGroup
                key={`others-${searching}`}
                label={t(($) => $.actions.other_agents)}
                agents={filteredOthers}
                defaultOpen={searching}
                skillIds={skillIds}
                selectedIds={selectedIds}
                onToggle={handleToggle}
              />
            </>
          )}
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => handleOpenChange(false)}
            disabled={saving}
          >
            {t(($) => $.actions.cancel)}
          </Button>
          <Button
            type="button"
            onClick={handleConfirm}
            disabled={saving || count === 0}
          >
            {saving ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                {t(($) => $.actions.adding)}
              </>
            ) : (
              t(($) => $.actions.add_confirm, { num: count })
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Delete confirmation (single row and batch share one dialog)
// ---------------------------------------------------------------------------

export function DeleteSkillsDialog({
  rows,
  ctx,
  open,
  onOpenChange,
  onDeleted,
}: {
  rows: SkillRow[];
  ctx: SkillActionsContext;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onDeleted?: () => void;
}) {
  const { t } = useT("skills");
  const qc = useQueryClient();
  const [deleting, setDeleting] = useState(false);
  const single = rows.length === 1 ? rows[0] : null;
  const count = rows.length;

  const handleConfirm = async () => {
    setDeleting(true);
    try {
      for (const row of rows) {
        await api.deleteSkill(row.skill.id);
      }
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(ctx.wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(ctx.wsId) });
      toast.success(t(($) => $.actions.deleted_toast, { count }));
      onOpenChange(false);
      onDeleted?.();
    } catch (e) {
      toast.error(
        e instanceof Error && e.message
          ? e.message
          : t(($) => $.actions.delete_failed_toast),
      );
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!deleting) onOpenChange(v);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {single
              ? t(($) => $.detail.delete_dialog.title)
              : t(($) => $.actions.delete_dialog_title, { count })}
          </DialogTitle>
          <DialogDescription>
            {single
              ? single.agents.length > 0
                ? t(($) => $.detail.delete_dialog.description_with_agents, {
                    name: single.skill.name,
                    count: single.agents.length,
                  })
                : t(($) => $.detail.delete_dialog.description_no_agents, {
                    name: single.skill.name,
                  })
              : t(($) => $.actions.delete_dialog_desc, { count })}
          </DialogDescription>
        </DialogHeader>
        <div className="rounded-md bg-destructive/10 px-3 py-2 text-xs text-destructive">
          {t(($) => $.detail.delete_dialog.warning)}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={deleting}
          >
            {t(($) => $.detail.delete_dialog.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={handleConfirm}
            disabled={deleting}
          >
            {deleting ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                {t(($) => $.detail.delete_dialog.deleting)}
              </>
            ) : (
              <>
                <Trash2 className="h-3 w-3" />
                {t(($) => $.detail.delete_dialog.confirm)}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Row kebab
// ---------------------------------------------------------------------------

// The row is a plain `<div>` whose whole-row navigation is a mouse `onClick`
// (see `useRowLink`), not an ancestor `<a>`. The wrapper span stops click
// propagation so opening this menu never navigates the row — just
// stopPropagation, no preventDefault (there is no native anchor to cancel).
export function SkillRowActions({
  row,
  ctx,
}: {
  row: SkillRow;
  ctx: SkillActionsContext;
}) {
  const { t } = useT("skills");
  const [addOpen, setAddOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  return (
    <span
      onClick={(e) => e.stopPropagation()}
      className="flex items-center"
    >
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label={t(($) => $.actions.row_menu)}
              className="flex size-7 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-accent-foreground group-hover/row:opacity-100 data-popup-open:bg-accent data-popup-open:opacity-100 data-popup-open:text-accent-foreground"
            >
              <MoreHorizontal className="size-4" />
            </button>
          }
        />
        <DropdownMenuContent align="end" className="w-52">
          <DropdownMenuItem onClick={() => setAddOpen(true)}>
            <Plus className="size-3.5" />
            {t(($) => $.actions.add_to_agent)}
          </DropdownMenuItem>
          {row.canEdit && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                onClick={() => setDeleteOpen(true)}
              >
                <Trash2 className="size-3.5" />
                {t(($) => $.actions.delete)}
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      <AddToAgentDialog
        skills={[row.skill]}
        ctx={ctx}
        open={addOpen}
        onOpenChange={setAddOpen}
      />
      <DeleteSkillsDialog
        rows={[row]}
        ctx={ctx}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
      />
    </span>
  );
}

// ---------------------------------------------------------------------------
// Batch toolbar (floating bottom bar, same shape as the issues batch bar)
// ---------------------------------------------------------------------------

export function SkillBatchToolbar({
  rows,
  ctx,
  onClear,
}: {
  rows: SkillRow[];
  ctx: SkillActionsContext;
  onClear: () => void;
}) {
  const { t } = useT("skills");
  const [addOpen, setAddOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  if (rows.length === 0) return null;

  const allDeletable = rows.every((r) => r.canEdit);

  const deleteButton = (
    <Button
      variant="ghost"
      size="sm"
      disabled={!allDeletable}
      onClick={() => setDeleteOpen(true)}
      className={cn(
        "text-destructive hover:text-destructive",
        !allDeletable && "pointer-events-none",
      )}
    >
      <Trash2 className="mr-1 size-3.5" />
      {t(($) => $.actions.delete)}
    </Button>
  );

  return (
    <>
      {/* Anchored to the page root (relative), NOT the viewport: with a
          sidebar/split pane open, viewport-centering sits visibly off the
          list's own center. Same rule for every future list page's batch
          toolbar. */}
      <div className="absolute bottom-6 left-1/2 z-50 flex -translate-x-1/2 items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg">
        <div className="mr-1 flex items-center gap-1.5 border-r pl-1 pr-2">
          <span className="text-sm font-medium">
            {t(($) => $.actions.selected, { count: rows.length })}
          </span>
          <button
            type="button"
            aria-label={t(($) => $.actions.clear_selection)}
            onClick={onClear}
            className="rounded p-0.5 transition-colors hover:bg-accent"
          >
            <X className="size-3.5 text-muted-foreground" />
          </button>
        </div>

        <Button variant="ghost" size="sm" onClick={() => setAddOpen(true)}>
          <Plus className="mr-1 size-3.5" />
          {t(($) => $.actions.add_to_agent)}
        </Button>

        {allDeletable ? (
          deleteButton
        ) : (
          <Tooltip>
            <TooltipTrigger
              render={<span className="inline-flex">{deleteButton}</span>}
            />
            <TooltipContent side="top">
              {t(($) => $.actions.delete_no_permission)}
            </TooltipContent>
          </Tooltip>
        )}
      </div>

      <AddToAgentDialog
        skills={rows.map((r) => r.skill)}
        ctx={ctx}
        open={addOpen}
        onOpenChange={setAddOpen}
      />
      <DeleteSkillsDialog
        rows={rows}
        ctx={ctx}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        onDeleted={onClear}
      />
    </>
  );
}
