"use client";

// TECH-3582 — the Workspace Copy console (a workspace Settings tab).
//
// A one-time, admin-only console for merging one Multica workspace into
// another: pick a target workspace, then copy individual entities (issues,
// channels, projects, agents, chats, autopilots) into it. Every copy is
// non-destructive — the source workspace is never modified (backend store.go).
//
// Issue-shaped copies (issue/channel) carry parent/project links that the
// backend only heals once both ends exist in the target, so the console also
// exposes a "Relink issues" button that runs that post-pass on demand. The
// per-item copy already fires it automatically after each issue/channel copy.
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Copy, CopyCheck, Link2, Loader2, ShieldOff, TriangleAlert } from "lucide-react";
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
import { workspaceListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects";
import { channelListOptions } from "@multica/core/channels";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { autopilotListOptions } from "@multica/core/autopilots";
import { issueListOptions } from "@multica/core/issues";

import { relinkIssues } from "../core/api";
import { useCopyToWorkspace } from "../core/queries";
import {
  CASCADE_CAPABLE,
  ISSUE_SHAPED,
  type WorkspaceCopyEntityType,
} from "../core/types";

interface TypeOption {
  value: WorkspaceCopyEntityType;
  label: string;
}

// The entity kinds the console offers. Channels and DMs are both issue-shaped
// (kind 'channel' / 'dm') and travel through the same backend copier; the
// console lists them as separate types so each can be picked and copied.
const TYPE_OPTIONS: TypeOption[] = [
  { value: "issue", label: "Issues" },
  { value: "channel", label: "Channels" },
  { value: "dm", label: "Direct messages" },
  { value: "project", label: "Projects" },
  { value: "agent", label: "Agents" },
  { value: "chat", label: "Chats" },
  { value: "autopilot", label: "Autopilots" },
];

// Hard cap on rows rendered at once — the source workspace can hold thousands
// of issues. The filter narrows the list; the count line shows what is hidden.
const ROW_CAP = 100;

interface CopyRow {
  id: string;
  label: string;
}

type RowStatus = "copying" | "done" | "error";

export function WorkspaceCopyConsole() {
  const wsId = useWorkspaceId();
  const { role, isLoading: isMemberLoading } = useCurrentMember(wsId);

  const [entityType, setEntityType] = useState<WorkspaceCopyEntityType>("issue");
  const [targetId, setTargetId] = useState<string>("");
  const [filter, setFilter] = useState("");
  const [statuses, setStatuses] = useState<Record<string, RowStatus>>({});
  const [relinking, setRelinking] = useState(false);
  const [cascade, setCascade] = useState(false);

  const { getActorName } = useActorName();
  const copyMutation = useCopyToWorkspace();

  const { data: workspaces = [] } = useQuery(workspaceListOptions());
  // Targets are every other workspace the user belongs to.
  const targets = useMemo(
    () => workspaces.filter((w) => w.id !== wsId),
    [workspaces, wsId],
  );

  // One query per entity type, only the selected one is enabled.
  const issues = useQuery({ ...issueListOptions(wsId), enabled: entityType === "issue" });
  const channels = useQuery({
    ...channelListOptions(wsId),
    enabled: entityType === "channel" || entityType === "dm",
  });
  const projects = useQuery({ ...projectListOptions(wsId), enabled: entityType === "project" });
  const agents = useQuery({ ...agentListOptions(wsId), enabled: entityType === "agent" });
  const chats = useQuery({ ...chatSessionsOptions(wsId), enabled: entityType === "chat" });
  const autopilots = useQuery({ ...autopilotListOptions(wsId), enabled: entityType === "autopilot" });

  // Only isLoading is read off the active query; the rows come from the memo
  // below. Typed structurally so the heterogeneous list results unify.
  const active: { isLoading: boolean } =
    {
      issue: issues,
      channel: channels,
      dm: channels,
      project: projects,
      agent: agents,
      chat: chats,
      autopilot: autopilots,
    }[entityType] ?? issues;

  const allRows: CopyRow[] = useMemo(() => {
    switch (entityType) {
      case "issue":
        return (issues.data ?? []).map((i) => ({
          id: i.id,
          label: `#${i.number} ${i.title}`,
        }));
      case "channel":
        return (channels.data ?? [])
          .filter((c) => c.kind === "channel")
          .map((c) => ({ id: c.id, label: c.title || "Untitled channel" }));
      case "dm":
        // DMs have no name of their own — label them by their participants so
        // the admin can tell which conversation is which.
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
      case "chat":
        return (chats.data ?? []).map((s) => ({
          id: s.id,
          label: s.title || "Untitled chat",
        }));
      case "autopilot":
        return (autopilots.data ?? []).map((a) => ({ id: a.id, label: a.title }));
      default:
        return [];
    }
  }, [entityType, issues.data, channels.data, projects.data, agents.data, chats.data, autopilots.data, getActorName]);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return allRows;
    return allRows.filter((r) => r.label.toLowerCase().includes(q));
  }, [allRows, filter]);

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

  const copyOne = (row: CopyRow) => {
    if (!targetId) {
      toast.error("Pick a target workspace first.");
      return;
    }
    const withCascade = cascade && CASCADE_CAPABLE.has(entityType);
    setStatuses((s) => ({ ...s, [row.id]: "copying" }));
    copyMutation.mutate(
      {
        wsId,
        input: { targetWorkspaceId: targetId, entityType, sourceId: row.id, cascade: withCascade },
      },
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

  const runRelink = async () => {
    if (!targetId) {
      toast.error("Pick a target workspace first.");
      return;
    }
    setRelinking(true);
    try {
      const res = await relinkIssues(wsId, targetId);
      toast.success(
        `Relinked ${res.parents_relinked ?? 0} parent and ${res.projects_relinked ?? 0} project links; ` +
          `rewrote references in ${res.issues_rewritten ?? 0} issues and ${res.comments_rewritten ?? 0} comments.`,
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Relink failed.");
    } finally {
      setRelinking(false);
    }
  };

  const targetName = targets.find((t) => t.id === targetId)?.name;

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-base font-semibold">Workspace copy</h2>
        <p className="text-sm text-muted-foreground">
          Copy individual items from this workspace into another workspace you
          belong to. Copies are non-destructive — nothing here is changed or
          removed.
        </p>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">Target workspace</Label>
          <Select value={targetId} onValueChange={(v) => setTargetId(v ?? "")}>
            <SelectTrigger className="w-[min(100%,18rem)]">
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

        <div className="space-y-1">
          <Label className="text-xs text-muted-foreground">Type</Label>
          <Select
            value={entityType}
            onValueChange={(v) => {
              if (!v) return;
              setEntityType(v as WorkspaceCopyEntityType);
              setFilter("");
              // Reset the cascade toggle so a choice made for one type never
              // silently carries into another.
              setCascade(false);
            }}
          >
            <SelectTrigger className="w-[min(100%,12rem)]">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TYPE_OPTIONS.map((t) => (
                <SelectItem key={t.value} value={t.value}>
                  {t.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {CASCADE_CAPABLE.has(entityType) && (
          <label className="flex items-center gap-2 pb-2 text-sm">
            <Checkbox
              checked={cascade}
              onCheckedChange={(v) => setCascade(v === true)}
            />
            <span>
              {entityType === "project"
                ? "Include all open issues in the project"
                : "Include all open sub-issues"}
            </span>
          </label>
        )}

        {ISSUE_SHAPED.has(entityType) && (
          <Button
            variant="outline"
            size="sm"
            disabled={!targetId || relinking}
            onClick={() => void runRelink()}
          >
            {relinking ? (
              <Loader2 className="mr-1 size-4 animate-spin" />
            ) : (
              <Link2 className="mr-1 size-4" />
            )}
            Relink issues
          </Button>
        )}
      </div>

      <Input
        placeholder="Filter by name…"
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        className="max-w-sm"
      />

      {ISSUE_SHAPED.has(entityType) && (
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <TriangleAlert className="size-3.5" />
          After copying issues and channels, the parent/project links heal
          automatically; use <strong>Relink issues</strong> to re-run that pass
          for the whole target.
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
                <TableHead>Name</TableHead>
                <TableHead className="w-28 text-right">Action</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((row) => {
                const status = statuses[row.id];
                return (
                  <TableRow key={row.id}>
                    <TableCell className="max-w-md">
                      <div className="truncate">{row.label}</div>
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant={status === "done" ? "ghost" : "outline"}
                        size="sm"
                        disabled={status === "copying"}
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
