"use client";

import { useState } from "react";
import { Bot, Tag, Check } from "lucide-react";
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
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useWorkspaceStore } from "@/features/workspace";
import { useIssueStore } from "@/features/issues";
import { useSuggestLabelsMutation, useIssueMutations } from "@/features/issues/mutations";

interface LabelSuggestion {
  name: string;
  existing: boolean;
  label_id?: string;
  color?: string;
}

interface IssueLabelResult {
  issue_id: string;
  suggestions: LabelSuggestion[];
}

interface Props {
  issueIds: string[];
  open: boolean;
  onClose: () => void;
}

export function AILabelModal({ issueIds, open, onClose }: Props) {
  const workspace = useWorkspaceStore((s) => s.workspace);
  const issues = useIssueStore((s) => s.issues);
  const suggestLabelsMut = useSuggestLabelsMutation();
  const { addIssueLabel } = useIssueMutations();

  const [results, setResults] = useState<IssueLabelResult[] | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [applying, setApplying] = useState(false);

  async function runSuggestions() {
    if (!workspace) return;
    setLoading(true);
    try {
      const res = await suggestLabelsMut.mutateAsync({ workspaceId: workspace.id, issueIds });
      setResults(res.results);
      const initialSelected = new Set<string>();
      res.results.forEach((r: IssueLabelResult) => {
        r.suggestions.forEach((s: LabelSuggestion) => {
          if (s.existing && s.label_id) {
            initialSelected.add(`${r.issue_id}::${s.label_id}`);
          }
        });
      });
      setSelected(initialSelected);
    } catch {
      toast.error("Failed to get label suggestions. Check AI settings.");
    } finally {
      setLoading(false);
    }
  }

  function toggleLabel(issueId: string, labelId: string) {
    const key = `${issueId}::${labelId}`;
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  async function applySelected() {
    if (!results) return;
    setApplying(true);
    try {
      for (const result of results) {
        for (const s of result.suggestions) {
          if (s.existing && s.label_id && selected.has(`${result.issue_id}::${s.label_id}`)) {
            await addIssueLabel(result.issue_id, { labelId: s.label_id });
          }
        }
      }
      toast.success("Labels applied");
      onClose();
    } catch {
      toast.error("Failed to apply labels");
    } finally {
      setApplying(false);
    }
  }

  function handleOpenChange(open: boolean) {
    if (!open) {
      setResults(null);
      setSelected(new Set());
      onClose();
    }
  }

  const issueMap = new Map(issues.map((i) => [i.id, i]));

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Bot className="h-4 w-4" />
            AI Label Suggestions
          </DialogTitle>
          <DialogDescription>
            AI will analyze {issueIds.length} issue{issueIds.length > 1 ? "s" : ""} and suggest labels.
            Only existing workspace labels can be applied.
          </DialogDescription>
        </DialogHeader>

        {!results ? (
          <div className="py-4 flex flex-col items-center gap-4">
            <Tag className="h-10 w-10 text-muted-foreground" />
            <p className="text-sm text-muted-foreground text-center">
              Click below to analyze issues and get label suggestions from AI.
            </p>
            <Button onClick={runSuggestions} disabled={loading}>
              {loading ? "Analyzing…" : "Get Suggestions"}
            </Button>
          </div>
        ) : (
          <ScrollArea className="max-h-[400px]">
            <div className="space-y-4 pr-2">
              {results.map((result) => {
                const issue = issueMap.get(result.issue_id);
                return (
                  <div key={result.issue_id} className="space-y-2">
                    <p className="text-sm font-medium truncate">
                      {issue?.title ?? result.issue_id}
                    </p>
                    {result.suggestions.length === 0 ? (
                      <p className="text-xs text-muted-foreground">No label suggestions</p>
                    ) : (
                      <div className="flex flex-wrap gap-2">
                        {result.suggestions.map((s, idx) => {
                          const key = `${result.issue_id}::${s.label_id ?? s.name}`;
                          const isSelected = s.label_id ? selected.has(`${result.issue_id}::${s.label_id}`) : false;
                          return (
                            <div
                              key={idx}
                              className="flex items-center gap-1.5"
                            >
                              {s.existing && s.label_id ? (
                                <Checkbox
                                  id={key}
                                  checked={isSelected}
                                  onCheckedChange={() => toggleLabel(result.issue_id, s.label_id!)}
                                />
                              ) : (
                                <span className="h-3.5 w-3.5 flex items-center justify-center opacity-40">
                                  <Check className="h-3 w-3" />
                                </span>
                              )}
                              <Badge
                                variant={s.existing ? "secondary" : "outline"}
                                className="text-xs"
                                style={s.color ? { borderColor: s.color, color: s.color } : undefined}
                              >
                                {s.name}
                              </Badge>
                              {!s.existing && (
                                <span className="text-xs text-muted-foreground">(new)</span>
                              )}
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </ScrollArea>
        )}

        {results && (
          <DialogFooter>
            <Button variant="outline" onClick={onClose} disabled={applying}>
              Cancel
            </Button>
            <Button onClick={applySelected} disabled={applying || selected.size === 0}>
              {applying ? "Applying…" : `Apply ${selected.size} label${selected.size !== 1 ? "s" : ""}`}
            </Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  );
}
