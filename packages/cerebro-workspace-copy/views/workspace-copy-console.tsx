"use client";

// TECH-3582 / TECH-3742 — the Workspace Copy console (a workspace Settings tab).
//
// A one-time, admin-only console for merging one Multica workspace into
// another. It walks the admin through the fixed merge order:
//
//   1. Foundation (do this first): labels, skills, connections + credentials,
//      roles/groups/permissions, GitHub, settings — one bulk button.
//   2. Agents → 3. Projects → 4. Issues/Channels/DMs → 5. Chats → 6. Autopilots,
//      each picked per item (or all at once) from the list below.
//
// Every copy is non-destructive — the source workspace is never modified
// (backend store.go). Name clashes on agents/skills are resolved per the chosen
// conflict policy (keep both / skip / overwrite). After issue copies the
// parent/project links and internal references heal automatically; the Relink
// buttons re-run those passes on demand for the whole target.
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Copy,
  CopyCheck,
  Inbox,
  Layers,
  Link2,
  Loader2,
  ShieldOff,
  TriangleAlert,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import { useCurrentMember } from "@multica/core/permissions";
import {
  workspaceListOptions,
  agentListOptions,
  skillListOptions,
} from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects";
import { channelListOptions } from "@multica/core/channels";
import { workspaceChatSessionsOptions } from "@multica/core/chat/queries";
import { autopilotListOptions } from "@multica/core/autopilots";
import { issueListOptions } from "@multica/core/issues";
import type { IssueStatus } from "@multica/core/types";

import {
  copyFoundation,
  copyInbox,
  copyWorkspaceArtifacts,
  relinkGroupAccess,
  relinkIssues,
} from "../core/api";
import { useCopyToWorkspace } from "../core/queries";
import {
  CASCADE_CAPABLE,
  CONFLICT_CAPABLE,
  ISSUE_SHAPED,
  type ConflictPolicy,
  type WorkspaceCopyEntityType,
} from "../core/types";

interface TypeOption {
  value: WorkspaceCopyEntityType;
  label: string;
  // step number in the fixed merge order (foundation is step 1).
  step: number;
}

// The per-item types, listed in the fixed merge order so the admin copies them
// top to bottom. Step numbers continue from the foundation (step 1).
const TYPE_OPTIONS: TypeOption[] = [
  { value: "agent", label: "Agents", step: 2 },
  { value: "skill", label: "Skills", step: 2 },
  { value: "project", label: "Projects", step: 3 },
  { value: "issue", label: "Issues", step: 4 },
  { value: "channel", label: "Channels", step: 4 },
  { value: "dm", label: "Direct messages", step: 4 },
  { value: "chat", label: "Chats", step: 5 },
  { value: "autopilot", label: "Autopilots", step: 6 },
];

const ISSUE_STATUSES: IssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

// Hard cap on rows *rendered* at once — the source workspace can hold thousands
// of issues. This caps the table only; "Copy all" still copies every row that
// matches the filter, not just the rendered ones. The count line shows what is
// hidden from the table.
const ROW_CAP = 100;

interface CopyRow {
  id: string;
  label: string;
  status?: IssueStatus;
}

type RowStatus = "copying" | "done" | "error";

export function WorkspaceCopyConsole() {
  const wsId = useWorkspaceId();
  const { role, isLoading: isMemberLoading } = useCurrentMember(wsId);

  const [entityType, setEntityType] = useState<WorkspaceCopyEntityType>("agent");
  const [targetId, setTargetId] = useState<string>("");
  const [filter, setFilter] = useState("");
  const [statuses, setStatuses] = useState<Record<string, RowStatus>>({});
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [relinking, setRelinking] = useState(false);
  const [foundationRunning, setFoundationRunning] = useState(false);
  const [docsRunning, setDocsRunning] = useState(false);
  const [inboxRunning, setInboxRunning] = useState(false);
  const [bulkRunning, setBulkRunning] = useState(false);
  const [cascade, setCascade] = useState(false);
  const [conflict, setConflict] = useState<ConflictPolicy>("skip");
  // Which issue statuses to show; all on by default. Only used for the issue
  // list (channels/dms/chats have no meaningful status filter).
  const [statusFilter, setStatusFilter] = useState<Set<IssueStatus>>(
    () => new Set(ISSUE_STATUSES),
  );

  const { getActorName } = useActorName();
  const copyMutation = useCopyToWorkspace();

  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  const targets = useMemo(
    () => workspaces.filter((w) => w.id !== wsId),
    [workspaces, wsId],
  );

  const issues = useQuery({ ...issueListOptions(wsId), enabled: entityType === "issue" });
  const channels = useQuery({
    ...channelListOptions(wsId),
    enabled: entityType === "channel" || entityType === "dm",
  });
  const projects = useQuery({ ...projectListOptions(wsId), enabled: entityType === "project" });
  const agents = useQuery({ ...agentListOptions(wsId), enabled: entityType === "agent" });
  const skills = useQuery({ ...skillListOptions(wsId), enabled: entityType === "skill" });
  // TECH-3766: list every chat in the workspace (creator-agnostic) so an
  // owner/admin can copy all chats, not just their own.
  const chats = useQuery({ ...workspaceChatSessionsOptions(wsId), enabled: entityType === "chat" });
  const autopilots = useQuery({ ...autopilotListOptions(wsId), enabled: entityType === "autopilot" });

  const active: { isLoading: boolean } =
    {
      agent: agents,
      skill: skills,
      project: projects,
      issue: issues,
      channel: channels,
      dm: channels,
      chat: chats,
      autopilot: autopilots,
    }[entityType] ?? agents;

  const allRows: CopyRow[] = useMemo(() => {
    switch (entityType) {
      case "issue":
        return (issues.data ?? []).map((i) => ({
          id: i.id,
          label: `#${i.number} ${i.title}`,
          status: i.status,
        }));
      case "channel":
        return (channels.data ?? [])
          .filter((c) => c.kind === "channel")
          .map((c) => ({ id: c.id, label: c.title || "Untitled channel" }));
      case "dm":
        return (channels.data ?? [])
          .filter((c) => c.kind === "dm")
          .map((c) => {
            const names = c.participants
              .map((p) => getActorName(p.user_type, p.user_id))
              .filter((n) => n && n.trim().length > 0);
            return {
              id: c.id,
              label: names.join(", ") || c.title || "Direct message",
            };
          });
      case "project":
        return (projects.data ?? []).map((p) => ({ id: p.id, label: p.title }));
      case "agent":
        return (agents.data ?? []).map((a) => ({ id: a.id, label: a.name }));
      case "skill":
        return (skills.data ?? []).map((sk) => ({ id: sk.id, label: sk.name }));
      case "chat":
        return (chats.data ?? []).map((sn) => ({
          id: sn.id,
          label: sn.title || "Untitled chat",
        }));
      case "autopilot":
        // Squad-assigned autopilots are tagged so the admin can tell them apart
        // (a squad assignee can't be carried — its squad is set up manually).
        return (autopilots.data ?? []).map((a) => ({
          id: a.id,
          label: a.assignee_type === "squad" ? `${a.title} (squad)` : a.title,
        }));
      default:
        return [];
    }
  }, [entityType, issues.data, channels.data, projects.data, agents.data, skills.data, chats.data, autopilots.data, getActorName]);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    let rows = allRows;
    if (entityType === "issue") {
      rows = rows.filter((r) => !r.status || statusFilter.has(r.status));
    }
    if (q) rows = rows.filter((r) => r.label.toLowerCase().includes(q));
    return rows;
  }, [allRows, filter, entityType, statusFilter]);

  const visible = filtered.slice(0, ROW_CAP);

  if (!wsId || isMemberLoading) {
    return (
      <div className="flex h-32 items-center justify-center text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }

  const isAdmin = role === "owner" || role === "admin";
  if (!isAdmin) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 p-6 text-center">
        <ShieldOff className="size-8 text-muted-foreground" />
        <h2 className="text-base font-medium">Access denied</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          Only workspace owners and admins can copy workspace contents.
        </p>
      </div>
    );
  }

  const requireTarget = (): boolean => {
    if (!targetId) {
      toast.error("Pick a target workspace first.");
      return false;
    }
    return true;
  };

  const copyOptions = (row: CopyRow) => ({
    targetWorkspaceId: targetId,
    entityType,
    sourceId: row.id,
    cascade: cascade && CASCADE_CAPABLE.has(entityType),
    conflict: CONFLICT_CAPABLE.has(entityType) ? conflict : undefined,
  });

  const copyOne = (row: CopyRow) => {
    if (!requireTarget()) return;
    setStatuses((s) => ({ ...s, [row.id]: "copying" }));
    copyMutation.mutate(
      { wsId, input: copyOptions(row) },
      {
        onSuccess: (res) => {
          setStatuses((s) => ({ ...s, [row.id]: "done" }));
          const extra = res.cascade_copied ?? 0;
          const cascadeNote = extra > 0 ? ` (+${extra} underneath)` : "";
          toast.success(
            res.already_copied
              ? `"${row.label}" was already copied.`
              : `Copied "${row.label}"${cascadeNote}.`,
          );
        },
        onError: (err) => {
          setStatuses((s) => ({ ...s, [row.id]: "error" }));
          toast.error(err.message || "Copy failed.");
        },
      },
    );
  };

  // Copy every selected row (or every filtered row when none are ticked) in one
  // sequential pass, so the admin doesn't click a button per item. "Copy all"
  // operates on `filtered` (all rows matching the current filter), never on the
  // render-capped `visible` slice — otherwise a workspace with >100 rows would
  // silently copy only the first 100 (TECH-3766).
  const copyMany = async () => {
    if (!requireTarget()) return;
    const rows = selected.size > 0 ? filtered.filter((r) => selected.has(r.id)) : filtered;
    if (rows.length === 0) {
      toast.error("Nothing to copy.");
      return;
    }
    setBulkRunning(true);
    let ok = 0;
    let failed = 0;
    for (const row of rows) {
      setStatuses((s) => ({ ...s, [row.id]: "copying" }));
      try {
        await copyMutation.mutateAsync({ wsId, input: copyOptions(row) });
        setStatuses((s) => ({ ...s, [row.id]: "done" }));
        ok++;
      } catch {
        setStatuses((s) => ({ ...s, [row.id]: "error" }));
        failed++;
      }
    }
    setBulkRunning(false);
    toast[failed > 0 ? "warning" : "success"](
      `Copied ${ok} item${ok === 1 ? "" : "s"}${failed > 0 ? `, ${failed} failed` : ""}.`,
    );
  };

  const runFoundation = async () => {
    if (!requireTarget()) return;
    setFoundationRunning(true);
    try {
      const r = await copyFoundation(wsId, targetId);
      toast.success(
        `Foundation copied: ${r.labels_copied ?? 0} labels, ${r.skills_copied ?? 0} skills, ` +
          `${r.connections_copied ?? 0} connections, ${r.credentials_copied ?? 0} credentials, ` +
          `${r.roles_copied ?? 0} roles, ${r.groups_copied ?? 0} groups, ${r.settings_rows_copied ?? 0} settings.`,
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Foundation copy failed.");
    } finally {
      setFoundationRunning(false);
    }
  };

  const runDocs = async () => {
    if (!requireTarget()) return;
    setDocsRunning(true);
    try {
      const r = await copyWorkspaceArtifacts(wsId, targetId);
      toast.success(`Copied ${r.cascade_copied ?? 0} workspace documents & notes (plus folders).`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Documents copy failed.");
    } finally {
      setDocsRunning(false);
    }
  };

  // Copy my inbox — a shortcut that copies exactly the issues the current user
  // has open in their inbox (what they actually work with), not the whole issue
  // archive. The backend resolves the set + heals links/files in one pass.
  const runInbox = async () => {
    if (!requireTarget()) return;
    setInboxRunning(true);
    try {
      const r = await copyInbox(wsId, targetId);
      const found = r.inbox_issues ?? 0;
      const copied = r.issues_copied ?? 0;
      const inboxed = r.inbox_items_created ?? 0;
      const already = found - copied;
      toast.success(
        found === 0
          ? "Your inbox is empty — nothing to copy."
          : `Copied your inbox: ${copied} of ${found} issue${found === 1 ? "" : "s"}` +
              `${already > 0 ? ` (${already} already in the target)` : ""}` +
              `${inboxed > 0 ? `, added ${inboxed} to your inbox there` : ""}.`,
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Inbox copy failed.");
    } finally {
      setInboxRunning(false);
    }
  };

  const runRelink = async () => {
    if (!requireTarget()) return;
    setRelinking(true);
    try {
      const res = await relinkIssues(wsId, targetId);
      const ga = await relinkGroupAccess(wsId, targetId);
      toast.success(
        `Relinked ${res.parents_relinked ?? 0} parent and ${res.projects_relinked ?? 0} project links; ` +
          `rewrote references in ${res.issues_rewritten ?? 0} issues, ${res.comments_rewritten ?? 0} comments, ` +
          `${res.agents_rewritten ?? 0} agents, ${res.skills_rewritten ?? 0} skills, ` +
          `${res.autopilots_rewritten ?? 0} autopilots, ${res.chat_messages_rewritten ?? 0} chat messages; ` +
          `${ga.group_agent_access_relinked ?? 0} group→agent grants; ` +
          `copied ${(res.artifact_files_materialized ?? 0) + (res.attachments_materialized ?? 0)} files into the target.`,
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Relink failed.");
    } finally {
      setRelinking(false);
    }
  };

  const targetName = targets.find((t) => t.id === targetId)?.name;
  const step = TYPE_OPTIONS.find((t) => t.value === entityType)?.step ?? 2;
  const allVisibleSelected = visible.length > 0 && visible.every((r) => selected.has(r.id));

  const toggleAll = (on: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      for (const r of visible) {
        if (on) next.add(r.id);
        else next.delete(r.id);
      }
      return next;
    });
  };

  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-base font-semibold">Workspace copy</h2>
        <p className="text-sm text-muted-foreground">
          Merge this workspace into another by copying its contents over in a
          fixed order. Copies are non-destructive — nothing here is changed or
          removed.
        </p>
      </div>

      <div className="space-y-1">
        <Label className="text-xs text-muted-foreground">Target workspace</Label>
        <Select value={targetId} onValueChange={(v) => setTargetId(v ?? "")}>
          <SelectTrigger className="w-[min(100%,22rem)]">
            <SelectValue placeholder="Pick a workspace…" />
          </SelectTrigger>
          <SelectContent>
            {targets.length === 0 ? (
              <SelectItem value="__none__" disabled>
                No other workspaces
              </SelectItem>
            ) : (
              targets.map((w) => (
                <SelectItem key={w.id} value={w.id}>
                  {w.name}
                </SelectItem>
              ))
            )}
          </SelectContent>
        </Select>
      </div>

      {/* Step 1 — foundation. Do this first so per-item copies resolve against
          the labels / skills / connections / roles that were carried over. */}
      <div className="rounded-md border bg-muted/30 p-4 space-y-2">
        <div className="flex items-center gap-2">
          <Layers className="size-4" />
          <h3 className="text-sm font-semibold">1. Foundation — do this first</h3>
        </div>
        <p className="text-sm text-muted-foreground">
          Labels, skills, connections + credentials, roles / groups /
          permissions, GitHub links and workspace settings. Run this once before
          copying agents, projects or issues.
        </p>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" disabled={!targetId || foundationRunning} onClick={() => void runFoundation()}>
            {foundationRunning ? <Loader2 className="mr-1 size-4 animate-spin" /> : <Layers className="mr-1 size-4" />}
            Copy foundation
          </Button>
          <Button size="sm" variant="outline" disabled={!targetId || docsRunning} onClick={() => void runDocs()}>
            {docsRunning ? <Loader2 className="mr-1 size-4 animate-spin" /> : <Copy className="mr-1 size-4" />}
            Copy documents &amp; notes
          </Button>
        </div>
      </div>

      {/* Copy my inbox — a shortcut that copies exactly the issues the admin has
          open in their own inbox (with their documents, files and links), without
          copying the whole issue archive. Run foundation first so labels/roles
          resolve; links and files heal inside this pass. */}
      <div className="rounded-md border bg-muted/30 p-4 space-y-2">
        <div className="flex items-center gap-2">
          <Inbox className="size-4" />
          <h3 className="text-sm font-semibold">Copy my inbox</h3>
        </div>
        <p className="text-sm text-muted-foreground">
          Copies every issue you currently have open in your inbox — with its
          documents, files and parent / blocks links — into the target, without
          copying the whole issue archive. Run <strong>Copy foundation</strong>{" "}
          first so labels and roles resolve.
        </p>
        <Button size="sm" disabled={!targetId || inboxRunning} onClick={() => void runInbox()}>
          {inboxRunning ? <Loader2 className="mr-1 size-4 animate-spin" /> : <Inbox className="mr-1 size-4" />}
          Copy my inbox
        </Button>
      </div>

      {/* Steps 2-6 — per-item copies in the fixed order. */}
      <div className="flex flex-wrap items-end gap-3">
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">{step}. Copy items</Label>
          <Select
            value={entityType}
            onValueChange={(v) => {
              if (!v) return;
              setEntityType(v as WorkspaceCopyEntityType);
              setFilter("");
              setCascade(false);
              setSelected(new Set());
            }}
          >
            <SelectTrigger className="w-[min(100%,14rem)]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TYPE_OPTIONS.map((t) => (
                <SelectItem key={t.value} value={t.value}>
                  {t.step}. {t.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {CONFLICT_CAPABLE.has(entityType) && (
          <div className="space-y-1">
            <Label className="text-xs text-muted-foreground">On name clash</Label>
            <Select value={conflict} onValueChange={(v) => v && setConflict(v as ConflictPolicy)}>
              <SelectTrigger className="w-[min(100%,12rem)]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="skip">Skip (keep target's)</SelectItem>
                <SelectItem value="keep_both">Keep both (rename)</SelectItem>
                <SelectItem value="overwrite">Overwrite target</SelectItem>
              </SelectContent>
            </Select>
          </div>
        )}

        {CASCADE_CAPABLE.has(entityType) && (
          <label className="flex items-center gap-2 pb-2 text-sm">
            <Checkbox checked={cascade} onCheckedChange={(v) => setCascade(v === true)} />
            <span>
              {entityType === "project"
                ? "Include all open issues in the project"
                : "Include all open sub-issues"}
            </span>
          </label>
        )}

        {ISSUE_SHAPED.has(entityType) && (
          <Button variant="outline" size="sm" disabled={!targetId || relinking} onClick={() => void runRelink()}>
            {relinking ? <Loader2 className="mr-1 size-4 animate-spin" /> : <Link2 className="mr-1 size-4" />}
            Relink &amp; heal references
          </Button>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <Input
          placeholder="Filter by name…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="max-w-sm"
        />
        <Button
          size="sm"
          disabled={!targetId || bulkRunning || filtered.length === 0}
          onClick={() => void copyMany()}
        >
          {bulkRunning ? <Loader2 className="mr-1 size-4 animate-spin" /> : <Copy className="mr-1 size-4" />}
          {selected.size > 0 ? `Copy ${selected.size} selected` : `Copy all ${filtered.length}`}
        </Button>
      </div>

      {/* status filter — only meaningful for the issue list. */}
      {entityType === "issue" && (
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <span className="text-xs text-muted-foreground">Status:</span>
          {ISSUE_STATUSES.map((st) => (
            <label key={st} className="flex items-center gap-1.5">
              <Checkbox
                checked={statusFilter.has(st)}
                onCheckedChange={(v) =>
                  setStatusFilter((prev) => {
                    const next = new Set(prev);
                    if (v === true) next.add(st);
                    else next.delete(st);
                    return next;
                  })
                }
              />
              <span className="capitalize">{st.replace("_", " ")}</span>
            </label>
          ))}
        </div>
      )}

      {ISSUE_SHAPED.has(entityType) && (
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <TriangleAlert className="size-3.5" />
          After copying issues and channels, parent/project links and internal
          references heal automatically; use <strong>Relink &amp; heal
          references</strong> to re-run that pass for the whole target.
        </p>
      )}

      {active.isLoading ? (
        <div className="text-sm text-muted-foreground">Loading…</div>
      ) : filtered.length === 0 ? (
        <div className="rounded-md border border-dashed p-8 text-center text-sm text-muted-foreground">
          Nothing to copy here.
        </div>
      ) : (
        <>
          <div className="text-xs text-muted-foreground">
            Showing {visible.length} of {filtered.length}
            {filtered.length !== allRows.length && ` (filtered from ${allRows.length})`}
            {targetName && ` → copying into ${targetName}`}
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-10">
                  <Checkbox
                    checked={allVisibleSelected}
                    onCheckedChange={(v) => toggleAll(v === true)}
                    aria-label="Select all shown"
                  />
                </TableHead>
                <TableHead>Name</TableHead>
                <TableHead className="w-28 text-right">Action</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((row) => {
                const status = statuses[row.id];
                return (
                  <TableRow key={row.id}>
                    <TableCell>
                      <Checkbox
                        checked={selected.has(row.id)}
                        onCheckedChange={(v) =>
                          setSelected((prev) => {
                            const next = new Set(prev);
                            if (v === true) next.add(row.id);
                            else next.delete(row.id);
                            return next;
                          })
                        }
                        aria-label={`Select ${row.label}`}
                      />
                    </TableCell>
                    <TableCell className="max-w-md">
                      <div className="truncate">{row.label}</div>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant={status === "done" ? "ghost" : "outline"}
                        size="sm"
                        disabled={status === "copying" || bulkRunning}
                        onClick={() => copyOne(row)}
                      >
                        {status === "copying" ? (
                          <Loader2 className="mr-1 size-4 animate-spin" />
                        ) : status === "done" ? (
                          <CopyCheck className="mr-1 size-4 text-green-600" />
                        ) : (
                          <Copy className="mr-1 size-4" />
                        )}
                        {status === "done" ? "Copied" : status === "error" ? "Retry" : "Copy"}
                      </Button>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </>
      )}
    </div>
  );
}
