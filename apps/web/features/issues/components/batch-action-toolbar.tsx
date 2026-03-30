"use client";

import { useState } from "react";
import { X, Trash2, Bot, UserMinus } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@/components/ui/popover";
import type { UpdateIssueRequest } from "@/shared/types";
import { ALL_STATUSES, STATUS_CONFIG, PRIORITY_ORDER, PRIORITY_CONFIG } from "@/features/issues/config";
import { useWorkspaceStore, useActorName } from "@/features/workspace";
import { useIssueStore } from "@/features/issues/store";
import { useIssueSelectionStore } from "@/features/issues/stores/selection-store";
import { api } from "@/shared/api";
import { StatusIcon } from "./status-icon";
import { PriorityIcon } from "./priority-icon";

export function BatchActionToolbar() {
  const selectedIds = useIssueSelectionStore((s) => s.selectedIds);
  const clear = useIssueSelectionStore((s) => s.clear);
  const count = selectedIds.size;

  const [statusOpen, setStatusOpen] = useState(false);
  const [priorityOpen, setPriorityOpen] = useState(false);
  const [assigneeOpen, setAssigneeOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [loading, setLoading] = useState(false);

  if (count === 0) return null;

  const ids = Array.from(selectedIds);

  const handleBatchUpdate = async (updates: UpdateIssueRequest) => {
    setLoading(true);
    try {
      await api.batchUpdateIssues(ids, updates);
      for (const id of ids) {
        useIssueStore.getState().updateIssue(id, updates);
      }
      toast.success(`Updated ${count} issue${count > 1 ? "s" : ""}`);
    } catch {
      toast.error("Failed to update issues");
      api.listIssues({ limit: 200 }).then((res) => {
        useIssueStore.getState().setIssues(res.issues);
      });
    } finally {
      setLoading(false);
    }
  };

  const handleBatchDelete = async () => {
    setLoading(true);
    try {
      await api.batchDeleteIssues(ids);
      for (const id of ids) {
        useIssueStore.getState().removeIssue(id);
      }
      clear();
      toast.success(`Deleted ${count} issue${count > 1 ? "s" : ""}`);
    } catch {
      toast.error("Failed to delete issues");
      api.listIssues({ limit: 200 }).then((res) => {
        useIssueStore.getState().setIssues(res.issues);
      });
    } finally {
      setLoading(false);
      setDeleteOpen(false);
    }
  };

  return (
    <>
      <div className="fixed bottom-6 left-1/2 -translate-x-1/2 z-50 flex items-center gap-1 rounded-lg border bg-background px-2 py-1.5 shadow-lg">
        <div className="flex items-center gap-1.5 pl-1 pr-2 border-r mr-1">
          <span className="text-sm font-medium">{count} selected</span>
          <button
            type="button"
            onClick={clear}
            className="rounded p-0.5 hover:bg-accent transition-colors"
          >
            <X className="size-3.5 text-muted-foreground" />
          </button>
        </div>

        {/* Status */}
        <Popover open={statusOpen} onOpenChange={setStatusOpen}>
          <PopoverTrigger
            render={
              <Button variant="ghost" size="sm" disabled={loading} />
            }
          >
            <StatusIcon status="todo" className="h-3.5 w-3.5 mr-1" />
            Status
          </PopoverTrigger>
          <PopoverContent align="center" className="w-44 p-1">
            {ALL_STATUSES.map((s) => {
              const cfg = STATUS_CONFIG[s];
              return (
                <button
                  key={s}
                  type="button"
                  onClick={() => {
                    handleBatchUpdate({ status: s });
                    setStatusOpen(false);
                  }}
                  className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm ${cfg.hoverBg} transition-colors`}
                >
                  <StatusIcon status={s} className="h-3.5 w-3.5" />
                  <span>{cfg.label}</span>
                </button>
              );
            })}
          </PopoverContent>
        </Popover>

        {/* Priority */}
        <Popover open={priorityOpen} onOpenChange={setPriorityOpen}>
          <PopoverTrigger
            render={
              <Button variant="ghost" size="sm" disabled={loading} />
            }
          >
            <PriorityIcon priority="high" className="mr-1" />
            Priority
          </PopoverTrigger>
          <PopoverContent align="center" className="w-44 p-1">
            {PRIORITY_ORDER.map((p) => {
              const cfg = PRIORITY_CONFIG[p];
              return (
                <button
                  key={p}
                  type="button"
                  onClick={() => {
                    handleBatchUpdate({ priority: p });
                    setPriorityOpen(false);
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                >
                  <PriorityIcon priority={p} />
                  <span>{cfg.label}</span>
                </button>
              );
            })}
          </PopoverContent>
        </Popover>

        {/* Assignee */}
        <BatchAssigneePicker
          open={assigneeOpen}
          onOpenChange={setAssigneeOpen}
          onUpdate={handleBatchUpdate}
          loading={loading}
        />

        {/* Delete */}
        <Button
          variant="ghost"
          size="sm"
          disabled={loading}
          onClick={() => setDeleteOpen(true)}
          className="text-destructive hover:text-destructive"
        >
          <Trash2 className="size-3.5 mr-1" />
          Delete
        </Button>
      </div>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Delete {count} issue{count > 1 ? "s" : ""}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. This will permanently delete the
              selected issue{count > 1 ? "s" : ""} and all associated data.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleBatchDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function BatchAssigneePicker({
  open,
  onOpenChange,
  onUpdate,
  loading,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onUpdate: (updates: UpdateIssueRequest) => void;
  loading: boolean;
}) {
  const [filter, setFilter] = useState("");
  const members = useWorkspaceStore((s) => s.members);
  const agents = useWorkspaceStore((s) => s.agents);
  const { getActorInitials } = useActorName();

  const query = filter.toLowerCase();
  const filteredMembers = members.filter((m) =>
    m.name.toLowerCase().includes(query),
  );
  const filteredAgents = agents.filter((a) =>
    a.name.toLowerCase().includes(query),
  );

  return (
    <Popover
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v);
        if (!v) setFilter("");
      }}
    >
      <PopoverTrigger
        render={
          <Button variant="ghost" size="sm" disabled={loading} />
        }
      >
        Assignee
      </PopoverTrigger>
      <PopoverContent align="center" className="w-52 p-0">
        <div className="px-2 py-1.5 border-b">
          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Assign to..."
            className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          />
        </div>
        <div className="p-1 max-h-60 overflow-y-auto">
          <button
            type="button"
            onClick={() => {
              onUpdate({ assignee_type: null, assignee_id: null });
              onOpenChange(false);
            }}
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
          >
            <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="text-muted-foreground">Unassigned</span>
          </button>

          {filteredMembers.length > 0 && (
            <div>
              <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                Members
              </div>
              {filteredMembers.map((m) => (
                <button
                  key={m.user_id}
                  type="button"
                  onClick={() => {
                    onUpdate({ assignee_type: "member", assignee_id: m.user_id });
                    onOpenChange(false);
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                >
                  <div className="inline-flex size-4.5 shrink-0 items-center justify-center rounded-full bg-muted text-[8px] font-medium text-muted-foreground">
                    {getActorInitials("member", m.user_id)}
                  </div>
                  <span>{m.name}</span>
                </button>
              ))}
            </div>
          )}

          {filteredAgents.length > 0 && (
            <div>
              <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">
                Agents
              </div>
              {filteredAgents.map((a) => (
                <button
                  key={a.id}
                  type="button"
                  onClick={() => {
                    onUpdate({ assignee_type: "agent", assignee_id: a.id });
                    onOpenChange(false);
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                >
                  <div className="inline-flex size-4.5 shrink-0 items-center justify-center rounded-full bg-info/10 text-info">
                    <Bot className="size-2.5" />
                  </div>
                  <span>{a.name}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
