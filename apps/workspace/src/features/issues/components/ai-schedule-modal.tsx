"use client";

import { useState } from "react";
import { Bot, CalendarClock, Check } from "lucide-react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Badge } from "@/components/ui/badge";
import { useWorkspaceStore } from "@/features/workspace";
import { useIssueStore } from "@/features/issues";
import { useSuggestScheduleMutation, useIssueMutations } from "@/features/issues/mutations";

interface ScheduleSuggestion {
  issue_id: string;
  start_date: string;
  end_date: string;
  reason: string;
}

interface Props {
  issueIds: string[];
  open: boolean;
  onClose: () => void;
}

export function AIScheduleModal({ issueIds, open, onClose }: Props) {
  const workspace = useWorkspaceStore((s) => s.workspace);
  const issues = useIssueStore((s) => s.issues);
  const { mutateAsync: suggestSchedule } = useSuggestScheduleMutation();
  const { updateIssue } = useIssueMutations();

  const [suggestions, setSuggestions] = useState<ScheduleSuggestion[] | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [applying, setApplying] = useState(false);

  async function runSuggestions() {
    if (!workspace) return;
    setLoading(true);
    try {
      const res = await suggestSchedule({ workspaceId: workspace.id, issueIds });
      setSuggestions(res.suggestions);
      setSelected(new Set(res.suggestions.map((s) => s.issue_id)));
    } catch {
      toast.error("Failed to get schedule suggestions. Check AI settings.");
    } finally {
      setLoading(false);
    }
  }

  function toggleIssue(issueId: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(issueId)) next.delete(issueId);
      else next.add(issueId);
      return next;
    });
  }

  async function applySelected() {
    if (!suggestions) return;
    setApplying(true);
    try {
      for (const s of suggestions) {
        if (selected.has(s.issue_id)) {
          await updateIssue(s.issue_id, {
            start_date: s.start_date,
            due_date: s.end_date,
          });
        }
      }
      toast.success(`Applied schedule for ${selected.size} issue${selected.size !== 1 ? "s" : ""}`);
      onClose();
    } catch {
      toast.error("Failed to apply schedule");
    } finally {
      setApplying(false);
    }
  }

  function handleOpenChange(open: boolean) {
    if (!open) {
      setSuggestions(null);
      setSelected(new Set());
      onClose();
    }
  }

  const issueMap = new Map(issues.map((i) => [i.id, i]));

  function formatDate(d: string) {
    if (!d) return "—";
    try {
      return new Date(d).toLocaleDateString(undefined, { month: "short", day: "numeric" });
    } catch {
      return d;
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Bot className="h-4 w-4" />
            AI Schedule Suggestions
          </DialogTitle>
          <DialogDescription>
            AI will suggest start and end dates for {issueIds.length} issue{issueIds.length > 1 ? "s" : ""},
            factoring in priorities and dependencies.
          </DialogDescription>
        </DialogHeader>

        {!suggestions ? (
          <div className="py-4 flex flex-col items-center gap-4">
            <CalendarClock className="h-10 w-10 text-muted-foreground" />
            <p className="text-sm text-muted-foreground text-center">
              Click below to generate a scheduling plan using AI.
            </p>
            <Button onClick={runSuggestions} disabled={loading}>
              {loading ? "Planning…" : "Generate Schedule"}
            </Button>
          </div>
        ) : (
          <ScrollArea className="max-h-[400px]">
            <div className="space-y-3 pr-2">
              {suggestions.map((s) => {
                const issue = issueMap.get(s.issue_id);
                const isSelected = selected.has(s.issue_id);
                return (
                  <div
                    key={s.issue_id}
                    className={`flex items-start gap-3 rounded-md border p-3 transition-colors ${
                      isSelected ? "border-primary/40 bg-primary/5" : "border-border"
                    }`}
                  >
                    <Checkbox
                      checked={isSelected}
                      onCheckedChange={() => toggleIssue(s.issue_id)}
                      className="mt-0.5"
                    />
                    <div className="flex-1 min-w-0 space-y-1">
                      <p className="text-sm font-medium truncate">
                        {issue?.title ?? s.issue_id}
                      </p>
                      <div className="flex items-center gap-2">
                        <Badge variant="outline" className="text-xs font-normal">
                          {formatDate(s.start_date)} → {formatDate(s.end_date)}
                        </Badge>
                      </div>
                      {s.reason && (
                        <p className="text-xs text-muted-foreground">{s.reason}</p>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          </ScrollArea>
        )}

        {suggestions && (
          <DialogFooter>
            <Button variant="outline" onClick={onClose} disabled={applying}>
              Cancel
            </Button>
            <Button onClick={applySelected} disabled={applying || selected.size === 0}>
              {applying ? "Applying…" : `Apply to ${selected.size} issue${selected.size !== 1 ? "s" : ""}`}
            </Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  );
}
