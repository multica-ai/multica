"use client";

import { useState, useRef } from "react";
import { CalendarDays, Check, ChevronRight, Maximize2, Minimize2, UserMinus, X as XIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { toast } from "sonner";
import type { UpdateIssueRequest, IssueStatus, IssuePriority, IssueAssigneeType } from "@/shared/types";
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@/components/ui/popover";
import { Tooltip, TooltipTrigger, TooltipContent } from "@/components/ui/tooltip";
import { Button } from "@/components/ui/button";
import { ContentEditor, type ContentEditorRef } from "@/features/editor";
import { TitleEditor } from "@/features/editor";
import { StatusIcon, PriorityIcon, DueDatePicker, IssueDateTimePicker } from "@/features/issues/components";
import { ALL_STATUSES, STATUS_CONFIG, PRIORITY_ORDER, PRIORITY_CONFIG } from "@/features/issues/config";
import { useWorkspaceStore, useActorName } from "@/features/workspace";
import { useIssueMutations } from "@/features/issues/mutations";
import { useIssueStore } from "@/features/issues";
import { useIssueDraftStore } from "@/features/issues/stores/draft-store";
import { useFileUpload } from "@/shared/hooks/use-file-upload";
import { FileUploadButton } from "@/components/common/file-upload-button";
import { ActorAvatar } from "@/components/common/actor-avatar";
import { useRouter } from "@/shared/router";

// ---------------------------------------------------------------------------
// Pill trigger — shared rounded-full button style for toolbar
// ---------------------------------------------------------------------------

function PillButton({
  children,
  className,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
        "hover:bg-accent/60 transition-colors cursor-pointer",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}

function shortDateTime(date: string | null): string {
  if (!date) return "";

  return new Date(date).toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

// ---------------------------------------------------------------------------
// CreateIssueModal
// ---------------------------------------------------------------------------

export function CreateIssueModal({ onClose, data }: { onClose: () => void; data?: Record<string, unknown> | null }) {
  const router = useRouter();
  const workspaceName = useWorkspaceStore((s) => s.workspace?.name);
  const members = useWorkspaceStore((s) => s.members);
  const agents = useWorkspaceStore((s) => s.agents);
  const { getActorName } = useActorName();
  const { createIssue } = useIssueMutations();

  const draft = useIssueDraftStore((s) => s.draft);
  const setDraft = useIssueDraftStore((s) => s.setDraft);
  const clearDraft = useIssueDraftStore((s) => s.clearDraft);

  const [title, setTitle] = useState(draft.title);
  const descEditorRef = useRef<ContentEditorRef>(null);
  const [status, setStatus] = useState<IssueStatus>((data?.status as IssueStatus) || draft.status);
  const [priority, setPriority] = useState<IssuePriority>(draft.priority);
  const [submitting, setSubmitting] = useState(false);
  const [assigneeType, setAssigneeType] = useState<IssueAssigneeType | undefined>(draft.assigneeType);
  const [assigneeId, setAssigneeId] = useState<string | undefined>(draft.assigneeId);
  const [dueDate, setDueDate] = useState<string | null>(draft.dueDate);
  const [startDate, setStartDate] = useState<string | null>(draft.startDate);
  const [endDate, setEndDate] = useState<string | null>(draft.endDate);
  const [isExpanded, setIsExpanded] = useState(false);

  // Assignee popover
  const [assigneeOpen, setAssigneeOpen] = useState(false);
  const [assigneeFilter, setAssigneeFilter] = useState("");

  // File upload
  const { uploadWithToast } = useFileUpload();
  const handleUpload = (file: File) => uploadWithToast(file);

  const assigneeQuery = assigneeFilter.toLowerCase();
  const filteredMembers = members.filter((m) => m.name.toLowerCase().includes(assigneeQuery));
  const filteredAgents = agents.filter((a) => !a.archived_at && a.name.toLowerCase().includes(assigneeQuery));

  const assigneeLabel =
    assigneeType && assigneeId
      ? getActorName(assigneeType, assigneeId)
      : "Assignee";

  const dueDateObj = dueDate ? new Date(dueDate) : undefined;

  // Sync field changes to draft store
  const updateTitle = (v: string) => { setTitle(v); setDraft({ title: v }); };
  const updateStatus = (v: IssueStatus) => { setStatus(v); setDraft({ status: v }); };
  const updatePriority = (v: IssuePriority) => { setPriority(v); setDraft({ priority: v }); };
  const updateAssignee = (type?: IssueAssigneeType, id?: string) => {
    setAssigneeType(type); setAssigneeId(id);
    setDraft({ assigneeType: type, assigneeId: id });
  };
  const updateDueDate = (v: string | null) => { setDueDate(v); setDraft({ dueDate: v }); };
  const updateStartDate = (v: string | null) => { setStartDate(v); setDraft({ startDate: v }); };
  const updateEndDate = (v: string | null) => { setEndDate(v); setDraft({ endDate: v }); };
  const handleDateUpdate = (updates: Partial<UpdateIssueRequest>) => {
    if ("start_date" in updates) {
      updateStartDate(updates.start_date ?? null);
    }
    if ("end_date" in updates) {
      updateEndDate(updates.end_date ?? null);
    }
    if ("due_date" in updates) {
      updateDueDate(updates.due_date ?? null);
    }
  };

  const handleSubmit = async () => {
    if (!title.trim() || submitting) return;
    setSubmitting(true);
    try {
      const issue = await createIssue({
        title: title.trim(),
        description: descEditorRef.current?.getMarkdown()?.trim() || undefined,
        status,
        priority,
        assignee_type: assigneeType,
        assignee_id: assigneeId,
        start_date: startDate || undefined,
        end_date: endDate || undefined,
        due_date: dueDate || undefined,
      });
      clearDraft();
      onClose();
      toast.custom((t) => (
        <div className="bg-popover text-popover-foreground border rounded-lg shadow-lg p-4 w-90">
          <div className="flex items-center gap-2 mb-2">
            <div className="flex items-center justify-center size-5 rounded-full bg-emerald-500/15 text-emerald-500">
              <Check className="size-3" />
            </div>
            <span className="text-sm font-medium">Issue created</span>
          </div>
          <div className="flex items-center gap-2 text-sm text-muted-foreground ml-7">
            <StatusIcon status={issue.status} className="size-3.5 shrink-0" />
            <span className="truncate">{issue.identifier} – {issue.title}</span>
          </div>
          <button
            type="button"
            className="ml-7 mt-2 text-sm text-primary hover:underline cursor-pointer"
            onClick={() => {
              router.push(`/issues/${issue.id}`);
              toast.dismiss(t);
            }}
          >
            View issue
          </button>
        </div>
      ), { duration: 5000 });
    } catch {
      toast.error("Failed to create issue");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent
        showCloseButton={false}
        className={cn(
          "p-0 gap-0 flex flex-col overflow-hidden",
          "top-1/2! left-1/2! -translate-x-1/2!",
          "transition-all! duration-300! ease-out!",
          isExpanded
            ? "max-w-4xl! w-full! h-5/6! -translate-y-1/2!"
            : "max-w-2xl! w-full! h-96! -translate-y-1/2!",
        )}
      >
        <DialogTitle className="sr-only">New Issue</DialogTitle>

        {/* Header */}
        <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
          <div className="flex items-center gap-1.5 text-xs">
            <span className="text-muted-foreground">{workspaceName}</span>
            <ChevronRight className="size-3 text-muted-foreground/50" />
            <span className="font-medium">New issue</span>
          </div>
          <div className="flex items-center gap-1">
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    onClick={() => setIsExpanded(!isExpanded)}
                    className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                  </button>
                }
              />
              <TooltipContent side="bottom">{isExpanded ? "Collapse" : "Expand"}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <button
                    onClick={onClose}
                    aria-label="Close new issue dialog"
                    className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                  >
                    <XIcon className="size-4" />
                  </button>
                }
              />
              <TooltipContent side="bottom">Close</TooltipContent>
            </Tooltip>
          </div>
        </div>

        {/* Title */}
        <div className="px-5 pb-2 shrink-0">
          <TitleEditor
            autoFocus
            defaultValue={draft.title}
            placeholder="Issue title"
            className="text-lg font-semibold"
            onChange={(v) => updateTitle(v)}
            onSubmit={handleSubmit}
          />
        </div>

        {/* Description — takes remaining space */}
        <div className="flex-1 min-h-0 overflow-y-auto px-5">
          <ContentEditor
            ref={descEditorRef}
            defaultValue={draft.description}
            placeholder="Add description..."
            onUpdate={(md) => setDraft({ description: md })}
            onUploadFile={handleUpload}
            debounceMs={500}
          />
        </div>

        {/* Property toolbar */}
        <div className="flex items-center gap-1.5 px-4 py-2 shrink-0 flex-wrap">
          {/* Status */}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <PillButton>
                  <StatusIcon status={status} className="size-3.5" />
                  <span>{STATUS_CONFIG[status].label}</span>
                </PillButton>
              }
            />
            <DropdownMenuContent align="start" className="w-44">
              {ALL_STATUSES.map((s) => (
                <DropdownMenuItem key={s} onClick={() => updateStatus(s)}>
                  <StatusIcon status={s} className="size-3.5" />
                  <span>{STATUS_CONFIG[s].label}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>

          {/* Priority */}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <PillButton>
                  <PriorityIcon priority={priority} />
                  <span>{PRIORITY_CONFIG[priority].label}</span>
                </PillButton>
              }
            />
            <DropdownMenuContent align="start" className="w-44">
              {PRIORITY_ORDER.map((p) => (
                <DropdownMenuItem key={p} onClick={() => updatePriority(p)}>
                  <span className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}>
                    <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                    {PRIORITY_CONFIG[p].label}
                  </span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>

          {/* Assignee — Popover for search support */}
          <Popover open={assigneeOpen} onOpenChange={(v) => { setAssigneeOpen(v); if (!v) setAssigneeFilter(""); }}>
            <PopoverTrigger
              render={
                <PillButton>
                  {assigneeType && assigneeId ? (
                    <>
                      <ActorAvatar actorType={assigneeType} actorId={assigneeId} size={16} />
                      <span>{assigneeLabel}</span>
                    </>
                  ) : (
                    <span className="text-muted-foreground">Assignee</span>
                  )}
                </PillButton>
              }
            />
            <PopoverContent align="start" className="w-52 p-0">
              <div className="px-2 py-1.5 border-b">
                <input
                  type="text"
                  value={assigneeFilter}
                  onChange={(e) => setAssigneeFilter(e.target.value)}
                  placeholder="Assign to..."
                  className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
                />
              </div>
              <div className="p-1 max-h-60 overflow-y-auto">
                {/* Unassigned */}
                <button
                  type="button"
                  onClick={() => {
                    updateAssignee(undefined, undefined);
                    setAssigneeOpen(false);
                  }}
                  className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                >
                  <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="text-muted-foreground">Unassigned</span>
                </button>

                {/* Members */}
                {filteredMembers.length > 0 && (
                  <>
                    <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">Members</div>
                    {filteredMembers.map((m) => (
                      <button
                        type="button"
                        key={m.user_id}
                        onClick={() => {
                          updateAssignee("member", m.user_id);
                          setAssigneeOpen(false);
                        }}
                        className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                      >
                        <ActorAvatar actorType="member" actorId={m.user_id} size={16} />
                        <span>{m.name}</span>
                      </button>
                    ))}
                  </>
                )}

                {/* Agents */}
                {filteredAgents.length > 0 && (
                  <>
                    <div className="px-2 pt-2 pb-1 text-xs font-medium text-muted-foreground uppercase tracking-wider">Agents</div>
                    {filteredAgents.map((a) => (
                      <button
                        type="button"
                        key={a.id}
                        onClick={() => {
                          updateAssignee("agent", a.id);
                          setAssigneeOpen(false);
                        }}
                        className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent transition-colors"
                      >
                        <ActorAvatar actorType="agent" actorId={a.id} size={16} />
                        <span>{a.name}</span>
                      </button>
                    ))}
                  </>
                )}

                {filteredMembers.length === 0 && filteredAgents.length === 0 && assigneeFilter && (
                  <div className="px-2 py-3 text-center text-sm text-muted-foreground">No results</div>
                )}
              </div>
            </PopoverContent>
          </Popover>

          <IssueDateTimePicker
            field="start_date"
            dateTimeValue={startDate}
            onUpdate={handleDateUpdate}
            trigger={
              <PillButton>
                <CalendarDays className="size-3.5 text-muted-foreground" />
                {startDate ? (
                  <span>{shortDateTime(startDate)}</span>
                ) : (
                  <span className="text-muted-foreground">Start date</span>
                )}
              </PillButton>
            }
          />

          <IssueDateTimePicker
            field="end_date"
            dateTimeValue={endDate}
            onUpdate={handleDateUpdate}
            trigger={
              <PillButton>
                <CalendarDays className="size-3.5 text-muted-foreground" />
                {endDate ? (
                  <span>{shortDateTime(endDate)}</span>
                ) : (
                  <span className="text-muted-foreground">End date</span>
                )}
              </PillButton>
            }
          />

          <DueDatePicker
            dueDate={dueDate}
            onUpdate={handleDateUpdate}
            trigger={
              <PillButton>
                <CalendarDays className="size-3.5 text-muted-foreground" />
                {dueDateObj ? (
                  <span>{dueDateObj.toLocaleDateString("en-US", { month: "short", day: "numeric" })}</span>
                ) : (
                  <span className="text-muted-foreground">Due date</span>
                )}
              </PillButton>
            }
          />
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-4 py-3 border-t shrink-0">
          <FileUploadButton
            onSelect={(file) => descEditorRef.current?.uploadFile(file)}
          />
          <Button size="sm" onClick={handleSubmit} disabled={!title.trim() || submitting}>
            {submitting ? "Creating..." : "Create Issue"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
