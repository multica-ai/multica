"use client";

import { useState, useEffect } from "react";
import { FileCode, GitCommit, Loader2, RefreshCw, Copy, Check } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { api } from "@/shared/api";

interface AgentDiffPreviewProps {
  issueId: string;
  issueTitle: string;
}

export function AgentDiffPreview({ issueId, issueTitle }: AgentDiffPreviewProps) {
  const [diff, setDiff] = useState<string>("");
  const [hasChanges, setHasChanges] = useState(false);
  const [workDir, setWorkDir] = useState<string>("");
  const [loading, setLoading] = useState(false);
  const [commitDialogOpen, setCommitDialogOpen] = useState(false);
  const [commitMessage, setCommitMessage] = useState("");
  const [committing, setCommitting] = useState(false);
  const [copied, setCopied] = useState(false);

  const fetchDiff = async () => {
    setLoading(true);
    try {
      const result = await api.getIssueDiff(issueId);
      setDiff(result.diff);
      setHasChanges(result.has_changes);
      setWorkDir(result.work_dir ?? "");
    } catch {
      // No diff available.
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchDiff();
  }, [issueId]);

  const handleCommit = async () => {
    setCommitting(true);
    try {
      const result = await api.commitAgentChanges(
        issueId,
        commitMessage || undefined,
        workDir || undefined
      );
      toast.success("Changes committed: " + result.message);
      setCommitDialogOpen(false);
      setCommitMessage("");
      fetchDiff(); // Refresh diff.
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Commit failed");
    } finally {
      setCommitting(false);
    }
  };

  const handleCopy = () => {
    navigator.clipboard.writeText(diff);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  if (!hasChanges && !loading) return null;

  return (
    <div className="border rounded-lg overflow-hidden">
      <div className="flex items-center justify-between px-3 py-2 bg-muted/50 border-b">
        <div className="flex items-center gap-2 text-sm font-medium">
          <FileCode className="h-4 w-4" />
          Agent Changes
        </div>
        <div className="flex items-center gap-1">
          <Button
            variant="ghost"
            size="sm"
            onClick={fetchDiff}
            disabled={loading}
            className="h-7 w-7 p-0"
          >
            {loading ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <RefreshCw className="h-3.5 w-3.5" />
            )}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={handleCopy}
            className="h-7 w-7 p-0"
          >
            {copied ? (
              <Check className="h-3.5 w-3.5 text-green-500" />
            ) : (
              <Copy className="h-3.5 w-3.5" />
            )}
          </Button>
          <Button
            variant="default"
            size="sm"
            onClick={() => {
              setCommitMessage(`feat: agent changes for ${issueTitle}`);
              setCommitDialogOpen(true);
            }}
            disabled={!hasChanges}
            className="h-7 gap-1 text-xs"
          >
            <GitCommit className="h-3 w-3" />
            Commit
          </Button>
        </div>
      </div>

      {loading ? (
        <div className="p-4 flex items-center justify-center">
          <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
        </div>
      ) : (
        <ScrollArea className="max-h-[300px]">
          <pre className="p-3 text-xs font-mono whitespace-pre overflow-x-auto">
            {diff.split("\n").map((line, i) => {
              let className = "text-muted-foreground";
              if (line.startsWith("+") && !line.startsWith("+++"))
                className = "text-green-600 dark:text-green-400";
              else if (line.startsWith("-") && !line.startsWith("---"))
                className = "text-red-600 dark:text-red-400";
              else if (line.startsWith("@@"))
                className = "text-blue-600 dark:text-blue-400";
              else if (line.startsWith("diff ") || line.startsWith("index "))
                className = "text-muted-foreground font-semibold";
              return (
                <div key={i} className={className}>
                  {line}
                </div>
              );
            })}
          </pre>
        </ScrollArea>
      )}

      <Dialog open={commitDialogOpen} onOpenChange={setCommitDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Commit Agent Changes</DialogTitle>
            <DialogDescription>
              Commit the changes made by the agent with a message.
            </DialogDescription>
          </DialogHeader>
          <Input
            placeholder="Commit message"
            value={commitMessage}
            onChange={(e) => setCommitMessage(e.target.value)}
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setCommitDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleCommit} disabled={committing}>
              {committing ? (
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
              ) : (
                <GitCommit className="h-4 w-4 mr-2" />
              )}
              Commit
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
