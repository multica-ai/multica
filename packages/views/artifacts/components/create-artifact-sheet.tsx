"use client";

import * as React from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from "@multica/ui/components/ui/sheet";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useCreateArtifact } from "@multica/core/artifacts/mutations";
import type { ArtifactKind } from "@multica/core/types";
import { KIND_LABELS } from "./kind-icon";

const KINDS: ArtifactKind[] = ["report", "plan", "decision", "diagram", "note"];

type Scope =
  | { issueId: string; projectId?: undefined }
  | { issueId?: undefined; projectId: string };

export type CreateArtifactSheetProps = Scope & {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultKind?: ArtifactKind;
};

export function CreateArtifactSheet(props: CreateArtifactSheetProps) {
  const create = useCreateArtifact();

  const [kind, setKind] = React.useState<ArtifactKind>(
    props.defaultKind ?? "report",
  );
  const [title, setTitle] = React.useState("");
  const [body, setBody] = React.useState("");

  React.useEffect(() => {
    if (!props.open) {
      setKind(props.defaultKind ?? "report");
      setTitle("");
      setBody("");
    }
  }, [props.open, props.defaultKind]);

  const canSubmit = title.trim().length > 0 && !create.isPending;

  const handleCreate = async () => {
    await create.mutateAsync({
      kind,
      title: title.trim(),
      body,
      ...(props.issueId ? { issue_id: props.issueId } : {}),
      ...(props.projectId ? { project_id: props.projectId } : {}),
    });
    props.onOpenChange(false);
  };

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className="w-full sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>New artifact</SheetTitle>
          <SheetDescription>
            Create a typed document scoped to{" "}
            {props.issueId ? "this issue" : "this project"}.
          </SheetDescription>
        </SheetHeader>

        <div className="flex flex-1 flex-col gap-3 overflow-y-auto px-4">
          <div className="flex flex-col gap-1">
            <Label htmlFor="artifact-kind">Kind</Label>
            <Select
              value={kind}
              onValueChange={(v) => setKind(v as ArtifactKind)}
            >
              <SelectTrigger id="artifact-kind">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {KIND_LABELS[k]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="artifact-title">Title</Label>
            <Input
              id="artifact-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Short, descriptive title"
              autoFocus
            />
          </div>

          <div className="flex flex-1 flex-col gap-1">
            <Label htmlFor="artifact-body">Body (markdown)</Label>
            <Textarea
              id="artifact-body"
              value={body}
              onChange={(e) => setBody(e.target.value)}
              placeholder={
                kind === "diagram"
                  ? "```mermaid\nflowchart LR\n  A --> B\n```"
                  : "Markdown content"
              }
              className="min-h-[300px] flex-1 font-mono text-sm"
            />
          </div>
        </div>

        <SheetFooter className="flex-row justify-end border-t">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => props.onOpenChange(false)}
            disabled={create.isPending}
          >
            Cancel
          </Button>
          <Button size="sm" onClick={handleCreate} disabled={!canSubmit}>
            {create.isPending ? "Creating…" : "Create"}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
