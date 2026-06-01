"use client";

import { useState } from "react";
import { Upload } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { BulkCreateIssueItem, WorkspaceImportError, WorkspaceImportPayload } from "@/shared/types";
import { api } from "@/shared/api";
import { useIssueStore } from "@/features/issues";

// ---------------------------------------------------------------------------
// Parsers
// ---------------------------------------------------------------------------

export function parseTextInput(text: string): BulkCreateIssueItem[] {
  return text
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .map((title) => ({ title }));
}

export function parseCsvInput(text: string): BulkCreateIssueItem[] {
  const lines = text.split("\n").filter((l) => l.trim().length > 0);
  if (lines.length < 2) return [];

  const firstLine = lines[0] ?? "";
  const headers = firstLine
    .split(",")
    .map((h) => h.trim().toLowerCase().replace(/^"|"$/g, ""));

  const titleIdx = headers.indexOf("title");
  const descIdx = headers.indexOf("description");
  const priorityIdx = headers.indexOf("priority");
  const statusIdx = headers.indexOf("status");

  if (titleIdx === -1) return [];

  return lines.slice(1).flatMap((line) => {
    const cols = parseCsvLine(line);
    const title = cols[titleIdx]?.trim() ?? "";
    const item: BulkCreateIssueItem = { title };
    if (descIdx !== -1 && cols[descIdx]) item.description = cols[descIdx];
    if (priorityIdx !== -1 && cols[priorityIdx]) item.priority = cols[priorityIdx] as BulkCreateIssueItem["priority"];
    if (statusIdx !== -1 && cols[statusIdx]) item.status = cols[statusIdx] as BulkCreateIssueItem["status"];
    return [item];
  });
}

function parseCsvLine(line: string): string[] {
  const result: string[] = [];
  let current = "";
  let inQuotes = false;
  for (let i = 0; i < line.length; i++) {
    const ch = line[i];
    if (ch === '"') {
      inQuotes = !inQuotes;
    } else if (ch === "," && !inQuotes) {
      result.push(current.trim());
      current = "";
    } else {
      current += ch;
    }
  }
  result.push(current.trim());
  return result;
}

// ---------------------------------------------------------------------------
// Preview table
// ---------------------------------------------------------------------------

function PreviewTable({ items }: { items: BulkCreateIssueItem[] }) {
  if (items.length === 0) return null;
  return (
    <div className="max-h-52 overflow-y-auto rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-8 text-xs">#</TableHead>
            <TableHead className="text-xs">Title</TableHead>
            <TableHead className="text-xs">Priority</TableHead>
            <TableHead className="text-xs">Status</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((item, i) => (
            <TableRow key={i}>
              <TableCell className="text-muted-foreground text-xs">{i + 1}</TableCell>
              <TableCell className="max-w-xs truncate text-xs">{item.title}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{item.priority ?? "—"}</TableCell>
              <TableCell className="text-xs text-muted-foreground">{item.status ?? "—"}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

// ---------------------------------------------------------------------------
// BulkImportModal
// ---------------------------------------------------------------------------

interface BulkImportModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function BulkImportModal({ open, onOpenChange }: BulkImportModalProps) {
  const [mode, setMode] = useState<"text" | "csv">("text");
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const [serverErrors, setServerErrors] = useState<WorkspaceImportError[] | null>(null);

  const refreshIssues = useIssueStore((s) => s.fetch);

  const parsedItems =
    mode === "text" ? parseTextInput(input) : parseCsvInput(input);

  const canSubmit = parsedItems.length > 0 && !loading;

  function handleModeChange(newMode: string) {
    setMode(newMode as "text" | "csv");
    setInput("");
    setServerErrors(null);
  }

  function handleInputChange(value: string) {
    setInput(value);
    setServerErrors(null);
  }

  async function handleSubmit() {
    if (!canSubmit) return;
    setLoading(true);
    setServerErrors(null);
    try {
      const payload: WorkspaceImportPayload = {
        schema_version: "2026-05-31",
        source_type: "issue-csv",
        issues: parsedItems,
      };
      const dryRun = await api.dryRunWorkspaceImport(payload);
      if ((dryRun.errors?.length ?? 0) > 0) {
        setServerErrors(dryRun.errors ?? []);
        return;
      }

      const result = await api.applyWorkspaceImport(payload);
      if ((result.errors?.length ?? 0) > 0 || result.failed > 0) {
        setServerErrors(result.errors ?? []);
        return;
      }
      await refreshIssues();
      toast.success(`${result.created} issue${result.created === 1 ? "" : "s"} created`);
      setInput("");
      onOpenChange(false);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to import issues";
      toast.error(msg);
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>Import Issues</DialogTitle>
        </DialogHeader>

        <Tabs value={mode} onValueChange={handleModeChange}>
          <TabsList className="mb-3">
            <TabsTrigger value="text">Plain Text</TabsTrigger>
            <TabsTrigger value="csv">CSV</TabsTrigger>
          </TabsList>

          <TabsContent value="text" className="space-y-3">
            <p className="text-sm text-muted-foreground">
              Enter one issue title per line. Empty lines are ignored.
            </p>
            <Textarea
              placeholder={"Fix login bug\nAdd dark mode\nUpdate documentation"}
              className="min-h-32 font-mono text-sm"
              value={input}
              onChange={(e) => handleInputChange(e.target.value)}
            />
          </TabsContent>

          <TabsContent value="csv" className="space-y-3">
            <p className="text-sm text-muted-foreground">
              Paste CSV with a header row. Accepted columns:{" "}
              <code className="text-xs">title, description, priority, status</code>
            </p>
            <Textarea
              placeholder={"title,description,priority,status\nFix login bug,Affects OAuth flow,high,todo\nAdd dark mode,,medium,"}
              className="min-h-32 font-mono text-sm"
              value={input}
              onChange={(e) => handleInputChange(e.target.value)}
            />
          </TabsContent>
        </Tabs>

        {parsedItems.length > 0 && (
          <div className="space-y-1.5">
            <p className="text-sm font-medium">
              Preview —{" "}
              <span className="text-muted-foreground font-normal">
                {parsedItems.length} issue{parsedItems.length === 1 ? "" : "s"}
              </span>
            </p>
            <PreviewTable items={parsedItems} />
          </div>
        )}

        {input.trim().length > 0 && parsedItems.length === 0 && (
          <p className="text-sm text-muted-foreground">
            No valid issues found. Check your input.
          </p>
        )}

        {serverErrors && serverErrors.length > 0 && (
          <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
            <p className="font-medium mb-1">Import failed:</p>
            <ul className="list-disc list-inside space-y-0.5">
              {serverErrors.map((e) => (
                <li key={`${e.code}-${e.message}`}>
                  {e.message}
                </li>
              ))}
            </ul>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit}>
            {loading ? "Importing…" : `Import${parsedItems.length > 0 ? ` ${parsedItems.length}` : ""}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Import Issues trigger button
// ---------------------------------------------------------------------------

export function BulkImportButton() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        variant="outline"
        size="icon-sm"
        className="text-muted-foreground"
        onClick={() => setOpen(true)}
        title="Import Issues"
      >
        <Upload className="size-4" />
      </Button>
      <BulkImportModal open={open} onOpenChange={setOpen} />
    </>
  );
}
